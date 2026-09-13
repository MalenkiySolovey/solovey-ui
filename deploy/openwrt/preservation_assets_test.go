package openwrt

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSysupgradePreservationAssetsAreFixedAndFailClosed(t *testing.T) {
	keepRaw, err := os.ReadFile(filepath.Join("solovey-ui.keep"))
	if err != nil {
		t.Fatal(err)
	}
	selected := []string{}
	for _, line := range strings.Split(string(keepRaw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			selected = append(selected, line)
		}
	}
	sort.Strings(selected)
	want := []string{DefaultOpenWrtInstanceIDPath, DefaultPreservationSnapshot, DefaultPreservationMetadata}
	sort.Strings(want)
	if strings.Join(selected, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sysupgrade preservation inventory=%v want=%v", selected, want)
	}
	for _, forbidden := range []string{"-wal", "-shm", "/tmp/", "/var/run/", "update-cache", "release-artifact"} {
		if strings.Contains(string(keepRaw), forbidden) {
			t.Fatalf("sysupgrade inventory includes volatile authority %q", forbidden)
		}
	}

	hookRaw, err := os.ReadFile(filepath.Join("solovey-ui-upgrade.sh"))
	if err != nil {
		t.Fatal(err)
	}
	hook := string(hookRaw)
	for _, fact := range []string{"SUI_DB_FOLDER=", "SUI_DEPLOYMENT_KIND=", "SUI_COMPONENTS_INSTALLED_FILE=", "start-stop-daemon -S"} {
		if strings.Count(hook, fact) != 1 {
			t.Fatalf("helper deployment fact %s must have one invocation owner", fact)
		}
	}
	for _, required := range []string{
		`local helper=/usr/lib/solovey-ui/solovey-openwrt-preservation`,
		`solovey_ui_preservation_helper prepare || {`,
		`trap 'solovey_ui_finalize_preservation_backup "$?"' EXIT`,
		`local action=fail-backup`, `[ "$status" -ne 0 ] || action=complete-backup`,
		`solovey_ui_preservation_helper "$action" ||`,
		`start-stop-daemon -S -x "$helper" -c solovey-ui:solovey-ui -- "$@" 1>&2`,
		`Solovey UI preservation cleanup is deferred to startup reconciliation`,
		`sysupgrade_init_conffiles="solovey_ui_prepare_preservation $sysupgrade_init_conffiles solovey_ui_declare_preservation_inventory solovey_ui_validate_preservation_inventory"`,
		`[ -z "${CONF_IMAGE:-}" ] || {`, `[ "${INTERACTIVE:-0}" -eq 0 ] || {`,
		`[ "${SAVE_OVERLAY:-0}" -eq 0 ] || {`,
		`SUI_DB_FOLDER=/etc/solovey-ui/db`, `SUI_DEPLOYMENT_KIND=openwrt-package-managed`,
		`$0 == "/etc/solovey-ui/openwrt-instance-id" { instance++; next }`,
		`$0 == "/etc/solovey-ui/db/sysupgrade-preservation/database.db" { snapshot++; next }`,
		`$0 == "/etc/solovey-ui/db/sysupgrade-preservation/metadata.json" { metadata++; next }`,
		`unexpected == 0`, `Solovey UI logical preservation failed; sysupgrade is blocked`, "exit 1",
		`[ -z "${CONF_RESTORE:-}" ] || {`, `Solovey UI does not support native sysupgrade backup restore; no files were applied`,
	} {
		if !strings.Contains(hook, required) {
			t.Fatalf("sysupgrade hook is missing %q", required)
		}
	}
	for _, forbidden := range []string{"apk ", "opkg ", "sysupgrade $", "eval ", "sh -c"} {
		if strings.Contains(hook, forbidden) {
			t.Fatalf("sysupgrade hook exposes forbidden surface %q", forbidden)
		}
	}
	initRaw, err := os.ReadFile(filepath.Join("solovey-ui.init"))
	if err != nil {
		t.Fatal(err)
	}
	init := string(initRaw)
	if !strings.Contains(init, `"$SOLOVEY_PREPARE" || return 1`) ||
		!strings.Contains(init, `"$SOLOVEY_LIFECYCLE" startup-restore || return 1`) ||
		strings.Index(init, `startup-restore`) > strings.Index(init, `procd_open_instance root-broker`) {
		t.Fatal("OpenWrt init does not resolve preservation before opening procd instances")
	}
}

func TestProcdInjectsSemanticPackageManagedDeployment(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("solovey-openwrt-prepare"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		`chown root:solovey-ui /run/solovey-ui`, `chmod 0750 /run/solovey-ui`,
		`chown root:root /run/solovey-ui/diagnostics`, `chmod 0700 /run/solovey-ui/diagnostics`,
		`chown solovey-ui:solovey-ui /run/solovey-ui/server-protection`,
		`chmod 0700 /run/solovey-ui/server-protection`, `solovey-openwrt-durability || exit 1`,
	} {
		if !strings.Contains(content, required) {
			t.Errorf("preparation runtime ownership contract lacks %q", required)
		}
	}
}

func TestPackagePostInstallOwnsAndRepairsPersistentLayout(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "openwrt", "solovey-openwrt-prepare"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		"id -u solovey-ui",
		"id -g solovey-ui",
		"chown root:root /etc/solovey-ui",
		"chmod 0711 /etc/solovey-ui",
		"chown solovey-ui:solovey-ui /etc/solovey-ui/db",
		"chmod 0700 /etc/solovey-ui/db",
		"preservation_root=/etc/solovey-ui/db/sysupgrade-preservation",
		`[ -d "$preservation_root" ] && [ ! -L "$preservation_root" ]`,
		`repair_preservation_file "$preservation_root/database.db" 536870912`,
		`repair_preservation_file "$preservation_root/metadata.json" 16384`,
		`chown solovey-ui:solovey-ui "$path"`,
		`chmod 0600 "$path"`,
		"[ -d /etc/solovey-ui ] && [ ! -L /etc/solovey-ui ]",
		"[ -d /etc/solovey-ui/db ] && [ ! -L /etc/solovey-ui/db ]",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("package lifecycle is missing persistent contract %q", required)
		}
	}
	for _, forbidden := range []string{"chmod 777", "chmod 0755 /etc/solovey-ui", "chown root:solovey-ui /etc/solovey-ui", "chmod 0750 /etc/solovey-ui", "chown -R", "chmod -R"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("package lifecycle weakens persistent security with %q", forbidden)
		}
	}
}
