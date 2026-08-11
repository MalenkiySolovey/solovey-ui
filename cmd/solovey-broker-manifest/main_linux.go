//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"golang.org/x/sys/unix"
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
	proof, err := client("ssh-proof", proofPath, 0, 0, true, broker.RoleSSHProof)
	if err != nil {
		fatal(err)
	}
	proof.RequiredGroup = uint32(gid64)
	legacy, err := client("panel-legacy-root", panelPath, 0, 0, false, broker.RolePanel)
	if err != nil {
		fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: 1, Clients: []broker.ClientManifest{panel, legacy, proof}})
	if err != nil {
		fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		fatal(err)
	}
	temporary := manifestPath + ".incoming"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o640); err != nil {
		fatal(err)
	}
	if err := os.Chown(temporary, 0, 0); err != nil {
		_ = os.Remove(temporary)
		fatal(err)
	}
	if err := os.Rename(temporary, manifestPath); err != nil {
		_ = os.Remove(temporary)
		fatal(err)
	}
}

func client(name, path string, uid, gid uint32, anyIdentity bool, role broker.Role) (broker.ClientManifest, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(resolved) {
		return broker.ClientManifest{}, fmt.Errorf("broker client release link is invalid: %s", path)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return broker.ClientManifest{}, err
	}
	hash := sha256.New()
	if _, err := file.WriteTo(hash); err != nil {
		_ = file.Close()
		return broker.ClientManifest{}, err
	}
	if err := file.Close(); err != nil {
		return broker.ClientManifest{}, err
	}
	var stat unix.Stat_t
	if err := unix.Stat(resolved, &stat); err != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return broker.ClientManifest{}, fmt.Errorf("broker client executable is unsafe: %s", resolved)
	}
	result := broker.ClientManifest{Name: name, UID: uid, GID: gid, Executable: resolved,
		ExecutableDigest: hex.EncodeToString(hash.Sum(nil)), Device: uint64(stat.Dev), Inode: stat.Ino, Roles: []broker.Role{role}}
	if anyIdentity {
		result.AnyNonRootUID = true
		result.AnyGID = true
	} else {
		result.CgroupUnit = "solovey-ui.service"
	}
	return result, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "solovey broker manifest:", err)
	os.Exit(1)
}
