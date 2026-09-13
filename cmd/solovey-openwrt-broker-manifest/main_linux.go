//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const maxClientExecutableBytes = int64(256 << 20)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey OpenWrt broker manifest:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 || os.Geteuid() != 0 {
		return errors.New("OpenWrt broker manifest writer requires root and accepts no arguments")
	}
	account, err := user.Lookup(openwrt.PanelAccountName)
	if err != nil {
		return err
	}
	uid, err := parseID(account.Uid)
	if err != nil || uid == 0 {
		return errors.New("OpenWrt panel UID is invalid")
	}
	gid, err := parseID(account.Gid)
	if err != nil || gid == 0 {
		return errors.New("OpenWrt panel GID is invalid")
	}
	panel, err := executable(openwrt.PanelExecutablePath)
	if err != nil {
		return err
	}
	readiness, err := executable(openwrt.ReadinessExecutablePath)
	if err != nil {
		return err
	}
	proof, err := executable(openwrt.SSHProofExecutablePath)
	if err != nil {
		return err
	}
	dropbear, err := executable(openwrt.DropbearExecutablePath)
	if err != nil {
		return err
	}
	owner, err := deploymentidentity.LoadProcdInstalled()
	if err != nil || owner.ProcessUID != uid || owner.ProcessGID != gid {
		return errors.New("OpenWrt application owner contract differs from the panel account")
	}
	manifest, err := openwrt.ProcdBrokerManifest(owner, panel, readiness, proof, dropbear)
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	parent, err := os.Lstat(filepath.Dir(broker.RuntimeManifestPath))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o022 != 0 {
		return errors.New("OpenWrt broker manifest parent is unsafe")
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != 0 || parentStat.Gid != gid {
		return errors.New("OpenWrt broker manifest parent ownership is unsafe")
	}
	directory := filepath.Dir(broker.RuntimeManifestPath)
	file, err := os.CreateTemp(directory, ".broker-clients-*.incoming")
	if err != nil {
		return err
	}
	temporary := file.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chown(0, 0); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("OpenWrt broker manifest write failed")
	}
	if err := os.Rename(temporary, broker.RuntimeManifestPath); err != nil {
		return err
	}
	installed = true
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr = directoryFile.Sync()
	closeErr = directoryFile.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("OpenWrt broker manifest directory sync failed")
	}
	return nil
}

func executable(name string) (openwrt.ClientExecutableIdentity, error) {
	object, err := executableobject.Open(name, executableobject.Policy{
		MaxBytes:               maxClientExecutableBytes,
		RequireRegular:         true,
		RequireExecutable:      true,
		RequireRootOwner:       true,
		ForbiddenMode:          0o022,
		RequireTrustedAncestry: true,
		AncestryOwner:          0,
		AncestryForbiddenMode:  0o022,
	})
	if err != nil {
		return openwrt.ClientExecutableIdentity{}, err
	}
	defer object.Close()
	if err := object.Revalidate(); err != nil {
		return openwrt.ClientExecutableIdentity{}, err
	}
	identity := object.Identity()
	return openwrt.ClientExecutableIdentity{Path: name, SHA256: identity.Digest, Device: identity.Device, Inode: identity.Inode}, nil
}

func parseID(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err
}
