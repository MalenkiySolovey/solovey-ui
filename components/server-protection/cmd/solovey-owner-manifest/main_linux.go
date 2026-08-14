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

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	"golang.org/x/sys/unix"
)

const (
	installRoot       = "/usr/local/solovey-ui"
	releaseRoot       = installRoot + "/releases"
	currentRelease    = releaseRoot + "/current"
	profileMarker     = "/etc/solovey-ui/deployment-profile"
	instanceIDPath    = "/etc/solovey-ui/instance-id"
	serviceLink       = "/etc/systemd/system/solovey-ui.service"
	profileRoot       = "/usr/local/lib/solovey-ui/systemd"
	serviceUnit       = "solovey-ui.service"
	serviceIdentity   = "solovey-ui"
	serviceCgroup     = "/system.slice/solovey-ui.service"
	maxIdentityFile   = int64(64 << 10)
	maxExecutableFile = int64(512 << 20)
)

func main() {
	if len(os.Args) != 1 || os.Geteuid() != 0 {
		fatal(errors.New("owner manifest writer requires root and accepts no arguments"))
	}
	contract, err := installedContract()
	if err != nil {
		fatal(err)
	}
	data, err := json.Marshal(contract)
	if err != nil {
		fatal(err)
	}
	if err := atomicRootFile(deploymentidentity.InstalledContractPath, append(data, '\n'), 0o400); err != nil {
		fatal(err)
	}
}

func installedContract() (deploymentidentity.ApplicationOwnerContractV1, error) {
	profile, err := installedProfile()
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, err
	}
	fragment := filepath.Join(profileRoot, "solovey-ui-"+profile+".service")
	resolvedFragment, err := filepath.EvalSymlinks(serviceLink)
	if err != nil || resolvedFragment != fragment {
		return deploymentidentity.ApplicationOwnerContractV1{}, errors.New("active service profile differs from the installed deployment profile")
	}
	fragmentSHA, err := regularRootDigest(fragment, maxIdentityFile)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, fmt.Errorf("service profile: %w", err)
	}
	resolvedRelease, err := filepath.EvalSymlinks(currentRelease)
	if err != nil || filepath.Dir(resolvedRelease) != releaseRoot || filepath.Base(resolvedRelease) == "current" {
		return deploymentidentity.ApplicationOwnerContractV1{}, errors.New("active release link is unsafe")
	}
	executable := filepath.Join(resolvedRelease, "solovey-ui")
	executableSHA, err := regularRootDigest(executable, maxExecutableFile)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, fmt.Errorf("panel executable: %w", err)
	}
	commit, err := buildCommit(filepath.Join(resolvedRelease, "BUILD_INFO.txt"))
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, err
	}
	instanceID, err := loadOrCreateInstanceID()
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, err
	}
	uid, gid, err := serviceCredentials(profile)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, err
	}
	sourceRevision := "src-" + digestString(commit)
	artifactRevision := "art-" + executableSHA
	deploymentID := "dep-" + digestString(strings.Join([]string{instanceID, sourceRevision, artifactRevision, profile, fragmentSHA, executable}, "\x00"))
	return protectionruntime.ApplicationOwner(protectionruntime.ApplicationOwnerInput{
		InstanceID: instanceID, SourceRevision: sourceRevision, ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
		ServiceIdentity: serviceIdentity, SystemdUnit: serviceUnit, ServiceFragmentPath: fragment,
		ServiceUnitSHA256: fragmentSHA, ServiceControlGroup: serviceCgroup, ExecutablePath: executable,
		ExecutableSHA256: executableSHA, ProcessUID: uid, ProcessGID: gid,
	})
}

func installedProfile() (string, error) {
	data, err := boundedRootFile(profileMarker, 128, 0o644)
	if err != nil {
		return "", err
	}
	profile := strings.TrimSpace(string(data))
	switch profile {
	case "native-hardened", "native-network-advanced", "native-legacy-root":
		return profile, nil
	default:
		return "", errors.New("deployment profile marker is invalid")
	}
}

func buildCommit(name string) (string, error) {
	data, err := boundedRootFile(name, maxIdentityFile, 0o644)
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

func serviceCredentials(profile string) (uint32, uint32, error) {
	if profile == "native-legacy-root" {
		return 0, 0, nil
	}
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		return 0, 0, err
	}
	uid, uidErr := strconv.ParseUint(account.Uid, 10, 32)
	gid, gidErr := strconv.ParseUint(account.Gid, 10, 32)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return 0, 0, errors.New("solovey-ui service account is invalid")
	}
	return uint32(uid), uint32(gid), nil
}

func loadOrCreateInstanceID() (string, error) {
	if data, err := boundedRootFile(instanceIDPath, 128, 0o400); err == nil {
		value := strings.TrimSpace(string(data))
		if validUUID(value) {
			return value, nil
		}
		return "", errors.New("installed instance identity is invalid")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := newInstanceID(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := atomicRootFile(instanceIDPath, []byte(value+"\n"), 0o400); err != nil {
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
		return "", errors.New("generated instance identity is invalid")
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
	stat, ok := info.Sys().(*unix.Stat_t)
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
	stat, ok := info.Sys().(*unix.Stat_t)
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
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return err
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
	directory, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "solovey-owner-manifest:", err)
	os.Exit(1)
}
