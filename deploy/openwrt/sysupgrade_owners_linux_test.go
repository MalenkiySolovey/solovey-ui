//go:build linux && !minimal

package openwrt

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Native archive selection is pinned OpenWrt. SQLite preparation/restoration
// runs in separate child processes with the installed production owner catalog.
// Mount admission is injected; this is not physical OpenWrt evidence.
func TestPinnedSysupgradeProductionDurableOwners(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("isolated chroot requires root")
	}
	for _, scenario := range []string{"clean", "file", "firmware", "failed_archive", "missing_inventory", "invalid_inventory", "missing_prepare_inventory", "rolled_back", "active_import_fenced", "missing_snapshot", "missing_metadata", "corrupt_snapshot", "partial_raw_db_wal"} {
		t.Run(scenario, func(t *testing.T) {
			source := newPreservationSysupgradeRoot(t)
			runChild := func(root, phase string) {
				t.Helper()
				command := exec.Command(os.Args[0], "-test.run=^TestSysupgradeProductionOwnerChild$", "-test.v")
				command.Env = append(os.Environ(), "SUI_SYSUPGRADE_CHILD_ROOT="+root, "SUI_SYSUPGRADE_CHILD_PHASE="+phase, "SUI_SYSUPGRADE_CHILD_SCENARIO="+scenario)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("owner child %s: %v\n%s", phase, err, output)
				}
			}
			runChild(source.root, "prepare")
			// Run the production owner composition in a native child under the
			// actual shipping hook environment. No helper environment is supplied
			// by the caller; physical mount admission alone remains host injected.
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			copyExecutable := func(from, to string) {
				data, err := os.ReadFile(from)
				if err != nil {
					t.Fatal(err)
				}
				source.writeMode(t, to, data, 0o755)
			}
			copyExecutable(executable, "/fixture-owner-tests")
			libraries, _ := exec.Command("ldd", executable).CombinedOutput()
			for _, library := range regexp.MustCompile(`/[^\s()]+`).FindAllString(string(libraries), -1) {
				copyExecutable(library, library)
			}
			source.write(t, "/usr/lib/solovey-ui/solovey-openwrt-preservation", `#!/bin/sh
set -eu
printf '%s|%s|%s|%s\n' "$1" "${SUI_DB_FOLDER:-}" "${SUI_DEPLOYMENT_KIND:-}" "${SUI_COMPONENTS_INSTALLED_FILE:-}" >> /tmp/owner-environment
SUI_SYSUPGRADE_CHILD_ROOT=/ SUI_SYSUPGRADE_HELPER_ACTION="$1" exec /fixture-owner-tests -test.run=^TestSysupgradeProductionOwnerChild$ -test.v
`)
			inventory := DefaultInstallRoot + "/components/installed.json"
			if scenario == "missing_prepare_inventory" {
				if err := os.Remove(source.hostPath(inventory)); err != nil {
					t.Fatal(err)
				}
				source.sysupgrade(t, []string{"-b", "-"}, false)
				return
			}
			if scenario == "missing_inventory" || scenario == "invalid_inventory" {
				// Remove/corrupt real authority only after preparation, at archive
				// creation. The helper itself must detect the loss during rehearsal.
				tar := string(source.read(t, "/bin/tar"))
				mutation := "rm " + inventory
				if scenario == "invalid_inventory" {
					mutation = "printf 'invalid-json' > " + inventory
				}
				source.write(t, "/bin/tar", strings.Replace(tar, "operation=", mutation+"\noperation=", 1))
			}
			arguments := []string{"-b", "-"}
			if scenario == "file" {
				arguments = []string{"-b", "/tmp/owners.tgz"}
			}
			if scenario == "firmware" {
				// Keep the pinned firmware entry/archive path intact. Hardware
				// validation and destructive handoff are isolated fixture adapters.
				source.write(t, "/usr/share/libubox/jshn.sh", `json_load() { :; }
json_get_var() { export "$1=1"; }
json_init() { :; }
json_add_string() { :; }
json_add_boolean() { :; }
json_add_object() { :; }
json_add_int() { :; }
json_close_object() { :; }
json_dump() { echo '{}'; }
`)
				source.write(t, "/lib/upgrade/platform.sh", "platform_check_image() { return 0; }\ninstall_bin() { :; }\n")
				source.write(t, "/usr/libexec/validate_firmware_image", "#!/bin/sh\necho '{}'\n")
				source.write(t, "/bin/ubus", "#!/bin/sh\necho handoff >> /tmp/firmware-handoff\n")
				source.write(t, "/tmp/firmware.img", "fixture image; never flashed\n")
				arguments = []string{"/tmp/firmware.img"}
			}
			var extra []string
			if scenario == "failed_archive" {
				extra = append(extra, "SUI_FAIL_TAR=1")
			}
			archive, diagnostic := source.sysupgrade(t, arguments, scenario != "failed_archive", extra...)
			if scenario == "file" {
				archive = source.read(t, "/tmp/owners.tgz")
			}
			if scenario == "firmware" {
				archive = source.read(t, "/tmp/sysupgrade.tgz")
				if string(source.read(t, "/tmp/firmware-handoff")) != "handoff\n" {
					t.Fatal("firmware handoff not reached")
				}
			}
			var terminal PreservationMetadataV1
			if err := json.Unmarshal(source.read(t, DefaultPreservationMetadata), &terminal); err != nil {
				t.Fatal(err)
			}
			if scenario == "missing_inventory" || scenario == "invalid_inventory" {
				if terminal.State != PreservationStatePrepared || !strings.Contains(string(diagnostic), "cleanup is deferred") {
					t.Fatal("missing inventory silently accepted")
				}
				if _, err := os.Stat(source.hostPath(DefaultPreservationSnapshot)); err != nil {
					t.Fatal("failed completion lost recovery snapshot")
				}
				return
			}
			wantState, action := PreservationStateBackupDone, "complete-backup"
			if scenario == "failed_archive" {
				wantState, action = PreservationStateBackupFail, "fail-backup"
			}
			if terminal.State != wantState {
				t.Fatalf("normal backup final state=%s, want BACKUP_COMPLETED; helper environment=%s; diagnostics=%s", terminal.State, source.read(t, "/tmp/owner-environment"), diagnostic)
			}
			environment := "|" + DefaultDatabaseFolder + "|openwrt-package-managed|" + inventory + "\n"
			if actual := string(source.read(t, "/tmp/owner-environment")); actual != "prepare"+environment+action+environment {
				t.Fatalf("helper deployment authority diverged: %s", actual)
			}
			if _, err := os.Stat(source.hostPath(DefaultPreservationSnapshot)); !os.IsNotExist(err) {
				t.Fatal("completed backup retained bulk snapshot")
			}
			runChild(source.root, "verify-live")
			if scenario == "failed_archive" {
				return
			}
			want := []string{DefaultOpenWrtInstanceIDPath, DefaultPreservationSnapshot, DefaultPreservationMetadata}
			slices.Sort(want)
			if !slices.Equal(sortedPreservationMembers(t, archive), want) {
				t.Fatal("native backup included unexpected Solovey state")
			}
			fresh := t.TempDir()
			extractPreservationArchive(t, archive, fresh)
			identity, err := os.ReadFile(fresh + DefaultOpenWrtInstanceIDPath)
			if err != nil || !bytes.Equal(identity, []byte(preservationKnownInstanceID+"\n")) {
				t.Fatal("stable UUID continuity failed")
			}
			for _, name := range []string{DefaultDatabaseFolder + "/solovey-ui.db", DefaultInstallRoot + "/solovey-ui", "/lib/apk/db/installed"} {
				if _, err := os.Stat(fresh + name); !os.IsNotExist(err) {
					t.Fatal("plain backup invented package or raw database")
				}
			}
			phase := "restore"
			switch scenario {
			case "missing_snapshot":
				if err := os.Remove(fresh + DefaultPreservationSnapshot); err != nil {
					t.Fatal(err)
				}
				phase = "reject"
			case "missing_metadata":
				if err := os.Remove(fresh + DefaultPreservationMetadata); err != nil {
					t.Fatal(err)
				}
				phase = "reject"
			case "corrupt_snapshot":
				if err := os.WriteFile(fresh+DefaultPreservationSnapshot, []byte("corrupt SQLite"), 0o600); err != nil {
					t.Fatal(err)
				}
				phase = "reject"
			case "partial_raw_db_wal":
				// A raw live DB is not the sealed logical snapshot, even if a
				// separately copied WAL sidecar accompanies it.
				if err := os.WriteFile(fresh+DefaultPreservationSnapshot, source.read(t, DefaultDatabaseFolder+"/solovey-ui.db"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fresh+DefaultPreservationSnapshot+"-wal", []byte("partial WAL"), 0o600); err != nil {
					t.Fatal(err)
				}
				phase = "reject"
			}
			runChild(fresh, phase)
		})
	}
}
