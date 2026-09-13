//go:build linux

package processevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const maxExecutableBytes int64 = 512 << 20

func Observe(pid int) (Fact, error) { return observeAt("/proc", pid) }

func observeAt(procRoot string, pid int) (Fact, error) {
	if pid <= 0 || !filepath.IsAbs(procRoot) || filepath.Clean(procRoot) != procRoot {
		return Fact{}, errors.New("process evidence target is invalid")
	}
	root := filepath.Join(procRoot, strconv.Itoa(pid))
	stat, err := readBounded(filepath.Join(root, "stat"), maxStatBytes)
	if err != nil {
		return Fact{}, err
	}
	parent, session, start, err := parseStat(stat)
	if err != nil {
		return Fact{}, err
	}
	status, err := readBounded(filepath.Join(root, "status"), maxStatusBytes)
	if err != nil {
		return Fact{}, err
	}
	uid, gid, groups, err := parseStatus(status)
	if err != nil {
		return Fact{}, err
	}
	cgroupAvailability, cgroups, cgroupRevision := observeCgroups(filepath.Join(root, "cgroup"))
	exeLink := filepath.Join(root, "exe")
	executable, err := os.Readlink(exeLink)
	deleted := strings.HasSuffix(executable, " (deleted)")
	if deleted {
		executable = strings.TrimSuffix(executable, " (deleted)")
	}
	if err != nil || !filepath.IsAbs(executable) {
		return Fact{}, errors.New("process executable label is unavailable")
	}
	executable = filepath.Clean(executable)
	file, err := os.Open(exeLink)
	if err != nil {
		return Fact{}, err
	}
	defer file.Close()
	before := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Dev == 0 || before.Ino == 0 || before.Size <= 0 || before.Size > maxExecutableBytes {
		return Fact{}, errors.New("process executable object is unavailable")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxExecutableBytes+1))
	if err != nil || written != before.Size {
		return Fact{}, errors.New("process executable object changed while hashing")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Fact{}, errors.New("process executable object cannot be rewound")
	}
	after := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || !sameExecutableObject(before, after) {
		return Fact{}, errors.New("process executable object changed after hashing")
	}
	finalStat, err := readBounded(filepath.Join(root, "stat"), maxStatBytes)
	if err != nil {
		return Fact{}, err
	}
	finalParent, finalSession, finalStart, err := parseStat(finalStat)
	if err != nil || finalParent != parent || finalSession != session || finalStart != start {
		return Fact{}, errors.New("process identity changed while observing executable")
	}
	fact := Fact{ProviderRevision: RevisionV2, PID: pid, ParentPID: parent, SessionID: session, StartTime: start,
		UID: uid, GID: gid, Groups: groups, Executable: executable, ExecutableDeleted: deleted,
		ExeDevice: uint64(before.Dev), ExeInode: before.Ino, ExeSize: before.Size, ExeMode: before.Mode,
		ExeUID: before.Uid, ExeGID: before.Gid, ExeDigest: hex.EncodeToString(hash.Sum(nil)),
		CgroupAvailability: cgroupAvailability, CgroupRevision: cgroupRevision, Cgroups: cgroups}
	payload, _ := json.Marshal(fact)
	sum := sha256.Sum256(payload)
	fact.Revision = hex.EncodeToString(sum[:])
	return fact, nil
}

func observeCgroups(path string) (CgroupAvailability, []CgroupEntry, string) {
	data, err := readBounded(path, maxCgroupBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CgroupUnavailable, nil, evidenceDigest([]byte(CgroupUnavailable))
		}
		return CgroupUnsafe, nil, evidenceDigest([]byte(CgroupUnsafe))
	}
	entries, err := parseCgroups(data)
	if err != nil {
		return CgroupMalformed, nil, evidenceDigest(append([]byte(CgroupMalformed+":"), data...))
	}
	return CgroupAvailable, entries, evidenceDigest(data)
}

func sameExecutableObject(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Size == right.Size && left.Mode == right.Mode &&
		left.Uid == right.Uid && left.Gid == right.Gid
}

func evidenceDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("process evidence exceeds its bound")
	}
	return data, nil
}
