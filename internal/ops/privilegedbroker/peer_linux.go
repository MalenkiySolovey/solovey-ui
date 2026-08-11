//go:build linux

package privilegedbroker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxPeerExecutableBytes int64 = 512 << 20

type ManifestAttestor struct{ Manifest Manifest }

func (a ManifestAttestor) Attest(_ context.Context, connection *net.UnixConn, role Role) (PeerIdentity, error) {
	if connection == nil {
		return PeerIdentity{}, errors.New("broker peer connection is absent")
	}
	var credential *unix.Ucred
	raw, err := connection.SyscallConn()
	if err != nil {
		return PeerIdentity{}, err
	}
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credential, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerIdentity{}, err
	}
	if socketErr != nil || credential == nil || credential.Pid <= 1 {
		return PeerIdentity{}, errors.New("broker peer credentials are unavailable")
	}
	identity, err := inspectPeer(int(credential.Pid), uint32(credential.Uid), uint32(credential.Gid), a.Manifest.Revision)
	if err != nil {
		return PeerIdentity{}, err
	}
	if _, ok := a.Manifest.matching(role, identity); !ok {
		return PeerIdentity{}, errors.New("broker peer does not match the release manifest")
	}
	return identity, nil
}

func (a ManifestAttestor) Recheck(_ context.Context, expected PeerIdentity, role Role) error {
	actual, err := inspectPeer(expected.PID, expected.UID, expected.GID, a.Manifest.Revision)
	if err != nil || actual.Revision != expected.Revision {
		return errors.New("broker peer changed after initial attestation")
	}
	if _, ok := a.Manifest.matching(role, actual); !ok {
		return errors.New("broker peer no longer matches the release manifest")
	}
	return nil
}

func inspectPeer(pid int, uid, gid uint32, manifestRevision string) (PeerIdentity, error) {
	root := filepath.Join("/proc", strconv.Itoa(pid))
	status, err := os.ReadFile(filepath.Join(root, "status"))
	if err != nil {
		return PeerIdentity{}, err
	}
	groups, err := validateStatus(status, uid, gid)
	if err != nil {
		return PeerIdentity{}, err
	}
	if err := validateInitialUserNamespace(root); err != nil {
		return PeerIdentity{}, err
	}
	executable, err := os.Readlink(filepath.Join(root, "exe"))
	if err != nil || strings.HasSuffix(executable, " (deleted)") || !filepath.IsAbs(executable) {
		return PeerIdentity{}, errors.New("broker peer executable is unstable")
	}
	executable = filepath.Clean(executable)
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPeerExecutableBytes {
		return PeerIdentity{}, errors.New("broker peer executable is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return PeerIdentity{}, errors.New("broker peer executable identity is unavailable")
	}
	digest, err := fileDigest(executable, info.Size())
	if err != nil {
		return PeerIdentity{}, err
	}
	startTime, err := processStartTime(root)
	if err != nil {
		return PeerIdentity{}, err
	}
	bootIDBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return PeerIdentity{}, err
	}
	bootID := strings.TrimSpace(string(bootIDBytes))
	if !safeIdentifier("boot-" + bootID) {
		return PeerIdentity{}, errors.New("broker peer boot identity is invalid")
	}
	cgroup, err := processCgroupUnit(root)
	if err != nil {
		return PeerIdentity{}, err
	}
	identity := PeerIdentity{PID: pid, UID: uid, GID: gid, Groups: groups, Executable: executable, ExecutableDigest: digest,
		Device: uint64(stat.Dev), Inode: stat.Ino, StartTime: startTime, CgroupUnit: cgroup,
		BootID: bootID, ManifestRevision: manifestRevision}
	identity.Revision = peerRevision(identity)
	return identity, nil
}

func validateStatus(data []byte, uid, gid uint32) ([]uint32, error) {
	wantedUID, wantedGID := strconv.FormatUint(uint64(uid), 10), strconv.FormatUint(uint64(gid), 10)
	uidOK, gidOK := false, false
	groups := make([]uint32, 0, 8)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "Uid":
			uidOK = len(fields) == 5 && fields[1] == wantedUID && fields[2] == wantedUID && fields[3] == wantedUID && fields[4] == wantedUID
		case "Gid":
			gidOK = len(fields) == 5 && fields[1] == wantedGID && fields[2] == wantedGID && fields[3] == wantedGID && fields[4] == wantedGID
		case "Groups":
			if len(fields) > 65 {
				return nil, errors.New("broker peer has too many supplementary groups")
			}
			for _, value := range fields[1:] {
				parsed, parseErr := strconv.ParseUint(value, 10, 32)
				if parseErr != nil {
					return nil, errors.New("broker peer supplementary group is malformed")
				}
				groups = append(groups, uint32(parsed))
			}
		}
	}
	if !uidOK || !gidOK {
		return nil, errors.New("broker peer UID or GID identity changed")
	}
	return groups, scanner.Err()
}

func validateInitialUserNamespace(root string) error {
	for _, name := range []string{"uid_map", "gid_map"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		fields := strings.Fields(string(data))
		if len(fields) != 3 || fields[0] != "0" || fields[1] != "0" {
			return errors.New("broker peer is in a remapped user namespace")
		}
		length, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil || length < 4294967295 {
			return errors.New("broker peer user namespace mapping is incomplete")
		}
	}
	return nil
}

func processStartTime(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return "", err
	}
	close := strings.LastIndexByte(string(data), ')')
	if close < 0 {
		return "", errors.New("broker peer process stat is malformed")
	}
	fields := strings.Fields(string(data)[close+1:])
	if len(fields) <= 19 {
		return "", errors.New("broker peer process start time is absent")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New("broker peer process start time is malformed")
	}
	return fields[19], nil
}

func processCgroupUnit(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "cgroup"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		for _, element := range strings.Split(parts[2], "/") {
			if (strings.HasSuffix(element, ".service") || strings.HasSuffix(element, ".scope")) && safeIdentifier(element) {
				return element, nil
			}
		}
	}
	return "", errors.New("broker peer systemd service identity is absent")
}

func fileDigest(path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxPeerExecutableBytes+1))
	if err != nil || written != size {
		return "", errors.New("broker peer executable changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func peerRevision(identity PeerIdentity) string {
	identity.Revision = ""
	data, _ := jsonMarshal(identity)
	return Digest(data)
}

func validateOwnedByRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("%s is not owned by root", path)
	}
	return nil
}

func jsonMarshal(value any) ([]byte, error) {
	// Kept here so the Linux identity code does not expose a mutable encoder.
	return json.Marshal(value)
}
