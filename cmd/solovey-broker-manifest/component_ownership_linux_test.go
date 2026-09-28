//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
)

func TestInstallerComponentOwnershipContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("canonical installer ownership gate requires root")
	}
	dir, err := os.MkdirTemp("/var/tmp", "solovey-components-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	installer, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "-c", `source "$1"
INSTALL_DIR="$2/app"; ENV_DIR="$2/etc"; SECRETBOX_ENV_FILE="$ENV_DIR/secretbox.env"
mkdir -p "$INSTALL_DIR"; chmod 755 "$INSTALL_DIR"
REQUESTED_PROFILE=full; COMPONENT_IDS=(telegram server-protection); resolve_binary_profile
umask 027
create_secretbox_env
[[ $(umask) == 0027 && $(stat -c %a "$SECRETBOX_ENV_FILE") == 600 ]]
# The failure path must restore the caller's mask as well.
SECRETBOX_ENV_FILE="$ENV_DIR/absent/secretbox.env"
if create_secretbox_env >/dev/null 2>&1; then exit 1; fi
[[ $(umask) == 0027 ]]
write_component_metadata 12345
COMPONENT_PAYLOAD_DIR="$2/payload"
mkdir -p "$COMPONENT_PAYLOAD_DIR/telegram/frontend/assets"
printf '{"id":"telegram","delivery":"in-process"}\n' > "$COMPONENT_PAYLOAD_DIR/telegram/component.json"
printf 'fixture asset\n' > "$COMPONENT_PAYLOAD_DIR/telegram/frontend/assets/fixture.js"
install_component_pack_dir telegram 12345
cp -a "$INSTALL_DIR/components" "$2/snapshot"
umask 077
write_component_metadata 12345
cp -a "$2/snapshot/." "$INSTALL_DIR/components/"
`, "bash", installer, dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer: %v: %s", err, output)
	}
	inventory := filepath.Join(dir, "app", "components", "installed.json")
	pack := filepath.Join(filepath.Dir(inventory), "telegram")
	for path, mode := range map[string]os.FileMode{filepath.Dir(inventory): 0o750, inventory: 0o640, pack: 0o750, filepath.Join(pack, "component.json"): 0o640, filepath.Join(pack, "frontend/assets/fixture.js"): 0o640} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 0 || stat.Gid != 12345 || info.Mode().Perm() != mode {
			t.Fatalf("unexpected metadata uid=%d gid=%d mode=%v", stat.Uid, stat.Gid, info.Mode())
		}
	}
	metadata, exists, err := installstate.Load(inventory)
	if err != nil || !exists || len(metadata.Components) == 0 {
		t.Fatalf("load: exists=%v err=%v", exists, err)
	}
	for _, member := range []bool{true, false} {
		gid := uint32(12346)
		if member {
			gid = 12345
		}
		script := `test ! -w "$1" && test ! -w "${1%/*}"`
		if member {
			script += ` && test -r "$1" && stat "$1" >/dev/null && cat "$1" >/dev/null`
		} else {
			script += ` && test ! -r "$1"`
		}
		command := exec.Command("bash", "-c", script, "bash", inventory)
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 12346, Gid: gid}}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("service-group=%v: %v: %s", member, err, output)
		}
	}
}
