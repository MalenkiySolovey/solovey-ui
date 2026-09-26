//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const (
	manifestPath = "/etc/solovey-ui/broker-clients.json"
	panelPath    = "/usr/local/solovey-ui/releases/current/solovey-ui"
	proofPath    = "/usr/local/solovey-ui/releases/current/solovey-ssh-proof"
)

func main() {
	if len(os.Args) != 1 || os.Geteuid() != 0 {
		fatal(errors.New("broker manifest writer requires root and accepts no arguments"))
	}
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		fatal(err)
	}
	uid64, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid64 == 0 {
		fatal(errors.New("solovey-ui account UID is invalid"))
	}
	gid64, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil || gid64 == 0 {
		fatal(errors.New("solovey-ui account GID is invalid"))
	}
	panel, err := client("panel", panelPath, uint32(uid64), uint32(gid64), false, broker.RolePanel)
	if err != nil {
		fatal(err)
	}
	proof, err := client("ssh-proof", proofPath, 0, uint32(gid64), true, broker.RoleSSHProof)
	if err != nil {
		fatal(err)
	}
	legacy, err := client("panel-legacy-root", panelPath, 0, 0, false, broker.RolePanel)
	if err != nil {
		fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{panel, legacy, proof}})
	if err != nil {
		fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		fatal(err)
	}
	if err := installManifest(manifestPath, append(data, '\n'), realManifestFilesystem(), true); err != nil {
		fatal(err)
	}
}

type manifestFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Chown(int, int) error
	Sync() error
	Close() error
}

// manifestFilesystem is command-local. It exists only to prove the ordered
// publication transaction and must not become a shared persistence manager.
type manifestFilesystem struct {
	lstat         func(string) (os.FileInfo, error)
	createTemp    func(string, string) (manifestFile, error)
	remove        func(string) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

func realManifestFilesystem() manifestFilesystem {
	return manifestFilesystem{
		lstat: os.Lstat,
		createTemp: func(directory, pattern string) (manifestFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		remove: os.Remove,
		rename: os.Rename,
		syncDirectory: func(path string) error {
			directory, err := os.Open(path)
			if err != nil {
				return err
			}
			return errors.Join(directory.Sync(), directory.Close())
		},
	}
}

func (f manifestFilesystem) valid() bool {
	return f.lstat != nil && f.createTemp != nil && f.remove != nil && f.rename != nil && f.syncDirectory != nil
}

func installManifest(path string, data []byte, fs manifestFilesystem, validateOwnership bool) error {
	path = filepath.Clean(path)
	if !fs.valid() || !filepath.IsAbs(path) || filepath.Base(path) != "broker-clients.json" || len(data) == 0 || len(data) > 256<<10 {
		return errors.New("broker manifest publication contract is invalid")
	}
	directory := filepath.Dir(path)
	parent, err := fs.lstat(directory)
	if err != nil || parent == nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o022 != 0 {
		return errors.New("broker manifest parent is unsafe")
	}
	if validateOwnership {
		stat, ok := parent.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Gid != 0 {
			return errors.New("broker manifest parent ownership is unsafe")
		}
	}
	file, err := fs.createTemp(directory, ".broker-clients-*.incoming")
	if err != nil {
		return err
	}
	temporary := file.Name()
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = fs.remove(temporary)
		}
	}()
	if err := file.Chmod(0o640); err != nil {
		return err
	}
	if validateOwnership {
		if err := file.Chown(0, 0); err != nil {
			return err
		}
	}
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := fs.rename(temporary, path); err != nil {
		return err
	}
	published = true
	if err := fs.syncDirectory(directory); err != nil {
		return err
	}
	return nil
}

func client(name, path string, uid, gid uint32, anyIdentity bool, role broker.Role) (broker.ClientManifest, error) {
	result := broker.ClientManifest{Name: name, UID: uid, GID: gid, Roles: []broker.Role{role},
		CgroupPolicy: broker.CgroupRequired, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1}
	if anyIdentity {
		result.UID, result.GID = 0, 0
		result.AnyNonRootUID, result.AnyGID, result.RequiredGroup = true, true, gid
	} else {
		result.CgroupUnit = "solovey-ui.service"
	}
	policy, err := broker.SystemdClientExecutablePolicy(result)
	if err != nil {
		return broker.ClientManifest{}, err
	}
	object, err := executableobject.Open(path, policy)
	if err != nil {
		return broker.ClientManifest{}, fmt.Errorf("broker client executable is unsafe: %s: %w", path, err)
	}
	defer object.Close()
	if err := object.Revalidate(); err != nil {
		return broker.ClientManifest{}, err
	}
	identity := object.Identity()
	result.Executable, result.ExecutableDigest, result.Device, result.Inode = identity.ResolvedPath, identity.Digest, identity.Device, identity.Inode
	return result, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "solovey broker manifest:", err)
	os.Exit(1)
}
