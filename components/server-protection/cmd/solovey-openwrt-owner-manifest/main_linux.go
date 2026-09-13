//go:build linux

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
)

const (
	openWrtBuildInfoPath    = openwrt.DefaultInstallRoot + "/BUILD_INFO.txt"
	openWrtServiceIdentity  = "solovey-ui-panel"
	maxIdentityFileBytes    = int64(64 << 10)
	maxPanelExecutableBytes = int64(512 << 20)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey server-protection OpenWrt owner manifest:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 || os.Geteuid() != 0 {
		return errors.New("OpenWrt owner manifest writer requires root and accepts no arguments")
	}
	account, err := user.Lookup(openwrt.PanelAccountName)
	if err != nil {
		return err
	}
	uid, err := parseAccountID(account.Uid)
	if err != nil || uid == 0 {
		return errors.New("OpenWrt panel UID is invalid")
	}
	gid, err := parseAccountID(account.Gid)
	if err != nil || gid == 0 {
		return errors.New("OpenWrt panel GID is invalid")
	}
	commit, err := buildCommit(openWrtBuildInfoPath)
	if err != nil {
		return err
	}
	panelDigest, err := regularRootDigest(openwrt.PanelExecutablePath, maxPanelExecutableBytes)
	if err != nil {
		return fmt.Errorf("panel executable: %w", err)
	}
	instanceID, err := loadOrCreateInstanceID()
	if err != nil {
		return err
	}
	sourceRevision := "src-" + digestString(commit)
	artifactRevision := "art-" + panelDigest
	deploymentID := "dep-" + digestString(strings.Join([]string{
		instanceID, sourceRevision, artifactRevision, openWrtServiceIdentity,
		openwrt.ProcdServiceName, openwrt.ProcdPanelInstance, openwrt.PanelExecutablePath,
	}, "\x00"))
	contract, err := protectionruntime.OpenWrtApplicationOwner(protectionruntime.OpenWrtApplicationOwnerInput{
		InstanceID: instanceID, SourceRevision: sourceRevision, ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
		ServiceIdentity: openWrtServiceIdentity, ProcdService: openwrt.ProcdServiceName, ProcdInstance: openwrt.ProcdPanelInstance,
		ExecutablePath: openwrt.PanelExecutablePath, ExecutableSHA256: panelDigest, ProcessUID: uid, ProcessGID: gid,
	})
	if err != nil {
		return err
	}
	data, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	if err := atomicRootFile(deploymentidentity.ProcdInstalledContractPath, append(data, '\n'), 0o444); err != nil {
		return err
	}
	mountProof, err := protectionruntime.ObserveRuntimeMount(protectionruntime.OpenWrtRuntimeRoot, protectionruntime.RuntimeMountVolatile)
	if err != nil {
		return err
	}
	runtimeRoot, err := protectionruntime.InstalledOpenWrtRuntimeRoot(contract, mountProof)
	if err != nil {
		return err
	}
	runtimeData, err := json.Marshal(runtimeRoot)
	if err != nil {
		return err
	}
	return atomicRootFile(protectionruntime.InstalledRuntimeRootPath, append(runtimeData, '\n'), 0o444)
}

func buildCommit(name string) (string, error) {
	data, err := boundedRootFile(name, maxIdentityFileBytes, 0o644)
	if err != nil {
		return "", err
	}
	commit := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "commit=") {
			if commit != "" {
				return "", errors.New("build metadata repeats commit")
			}
			commit = strings.TrimSpace(strings.TrimPrefix(line, "commit="))
		}
	}
	if commit == "" || len(commit) > 128 || strings.ContainsAny(commit, "\r\n\x00") {
		return "", errors.New("build metadata commit is invalid")
	}
	return commit, nil
}

func loadOrCreateInstanceID() (string, error) {
	if data, err := boundedRootFile(openwrt.DefaultOpenWrtInstanceIDPath, 128, 0o400); err == nil {
		value := strings.TrimSpace(string(data))
		if validUUID(value) {
			return value, nil
		}
		return "", errors.New("OpenWrt instance identity is invalid")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := newInstanceID(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := atomicRootFile(openwrt.DefaultOpenWrtInstanceIDPath, []byte(value+"\n"), 0o400); err != nil {
		return "", err
	}
	return value, nil
}

func newInstanceID(source io.Reader) (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(source, bytes); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	value := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
	if !validUUID(value) {
		return "", errors.New("generated OpenWrt instance identity is invalid")
	}
	return value, nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '4' || !strings.Contains("89ab", value[19:20]) {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && value == strings.ToLower(value)
}

func boundedRootFile(name string, limit int64, mode os.FileMode) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > limit || info.Mode().Perm() != mode {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("identity input file is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return nil, errors.New("identity input ownership is unsafe")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil || statErr != nil || closeErr != nil || int64(len(data)) > limit || !os.SameFile(info, after) {
		return nil, errors.New("identity input changed while reading")
	}
	return data, nil
}

func regularRootDigest(name string, limit int64) (string, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > limit || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("file is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return "", errors.New("file ownership is unsafe")
	}
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, readErr := io.Copy(hash, io.LimitReader(file, limit+1))
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil || statErr != nil || closeErr != nil || written != info.Size() || written > limit || !os.SameFile(info, after) {
		return "", errors.New("file changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func atomicRootFile(name string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(name)
	parent, err := os.Lstat(directory)
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o022 != 0 {
		return errors.New("OpenWrt owner contract parent is unsafe")
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != 0 || parentStat.Gid != 0 {
		return errors.New("OpenWrt owner contract parent ownership is unsafe")
	}
	temporary := name + ".incoming"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Chown(temporary, 0, 0); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, name); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = directoryFile.Sync()
	return errors.Join(err, directoryFile.Close())
}

func parseAccountID(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
