//go:build linux

package openwrt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture executes the pinned OpenWrt package helpers and procd shell
// adapter. It replaces only the external ubus daemon and installed Solovey
// executables; package ordering, rc.common dispatch and procd message
// construction remain the exact v25.12.5 sources.
func TestPinnedOpenWrtPackageAndProcdShellSemantics(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("pinned OpenWrt shell semantics require root for an isolated chroot")
	}
	pinned := pinnedOpenWrtSourceRoot(t)
	assertPinnedOpenWrtRevision(t, pinned)

	t.Run("install_reinstall_and_same_config_replacement", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		fixture.run(t, fixture.postInstallScript(t), nil, true)
		fixture.run(t, fixture.postInstallScript(t), nil, true)
		events := fixture.events(t)
		assertOrderedEvents(t, events, "prepare", "lifecycle:startup-restore", "instance:root-broker", "instance:panel", "ubus:set", "lifecycle:reconcile", "ubus:delete", "prepare", "lifecycle:startup-restore", "instance:root-broker", "instance:panel", "ubus:set")
		if countEvent(events, "instance:root-broker") != 4 || countEvent(events, "instance:panel") != 4 || countEvent(events, "ubus:set") != 4 {
			t.Fatalf("two post-install runs did not replace both registered instances through procd set: %s", events)
		}
		fixture.assertServiceAccount(t, "")
		fixture.assertRespawnAndEntrypoints(t, events)
	})

	t.Run("staged_root_only_enables_and_creates_account", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		fixture.installPinnedTree(t, "/stage")
		fixture.installPackageMetadata(t, "/stage")
		fixture.run(t, fixture.postInstallScript(t), map[string]string{"IPKG_INSTROOT": "/stage"}, true)
		events := fixture.events(t)
		if strings.Contains(events, "prepare") || strings.Contains(events, "instance:") || strings.Contains(events, "lifecycle:") || strings.Contains(events, "ubus:") {
			t.Fatalf("staged-root post-install started runtime authority: %s", events)
		}
		fixture.assertServiceAccount(t, "/stage")
		if _, err := os.Lstat(filepath.Join(fixture.root, "stage/etc/rc.d/S95solovey-ui")); err != nil {
			entries, _ := os.ReadDir(filepath.Join(fixture.root, "stage/etc/rc.d"))
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			t.Fatalf("pinned staged rc.common did not enable the service: %v; entries=%v", err, names)
		}
	})

	t.Run("upgrade_handoff_and_predeinstall_status", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		fixture.run(t, fixture.postInstallScript(t), nil, true)
		fixture.clearEvents(t)
		fixture.run(t, fixture.preUpgradeScript(t), nil, true)
		fixture.run(t, fixture.postUpgradeScript(t), nil, true)
		events := fixture.events(t)
		assertOrderedEvents(t, events, "ubus:delete", "prepare", "lifecycle:startup-restore", "instance:root-broker", "instance:panel", "ubus:set", "lifecycle:reconcile", "ubus:delete", "prepare", "lifecycle:startup-restore", "instance:root-broker", "instance:panel", "ubus:set")

		fixture.clearEvents(t)
		fixture.run(t, fixture.preDeinstallScript(t), map[string]string{"SUI_FAIL_UBUS_METHOD": "delete"}, true)
		events = fixture.events(t)
		assertOrderedEvents(t, events, "ubus:delete", "lifecycle:pre-remove")

		fixture.clearEvents(t)
		fixture.run(t, fixture.preDeinstallScript(t), map[string]string{"SUI_FAIL_PRE_REMOVE": "1"}, false)
		events = fixture.events(t)
		assertOrderedEvents(t, events, "ubus:delete", "lifecycle:pre-remove")
		if countEvent(events, "lifecycle:pre-remove") != 1 {
			t.Fatalf("package-specific pre-remove hook was not the final generated command: %s", events)
		}
	})

	t.Run("predeinstall_idempotent_absent_states", func(t *testing.T) {
		for _, state := range []string{"running", "stopped", "never-registered", "init-absent"} {
			t.Run(state, func(t *testing.T) {
				fixture := newPinnedLifecycleRoot(t, pinned)
				switch state {
				case "running":
					fixture.run(t, fixture.postInstallScript(t), nil, true)
				case "stopped":
					fixture.run(t, fixture.postInstallScript(t), nil, true)
					fixture.run(t, "/etc/init.d/solovey-ui stop", nil, true)
				case "init-absent":
					if err := os.Remove(fixture.hostPath("/etc/init.d/solovey-ui")); err != nil {
						t.Fatal(err)
					}
				}
				fixture.clearEvents(t)
				fixture.run(t, fixture.preDeinstallScript(t), nil, true)
				if countEvent(fixture.events(t), "lifecycle:pre-remove") != 1 {
					t.Fatalf("pre-remove hook count in %s state = %s", state, fixture.events(t))
				}
				fixture.run(t, fixture.preDeinstallScript(t), nil, true)
			})
		}
	})

	t.Run("preparation_failure_never_opens_an_instance", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		fixture.run(t, "/etc/init.d/solovey-ui start", map[string]string{"SUI_FAIL_PREPARE": "1"}, true)
		events := fixture.events(t)
		if strings.Contains(events, "instance:") {
			t.Fatalf("pinned rc.common opened procd state after preparation failure: %s", events)
		}
		if countEvent(events, "ubus:set") != 1 {
			t.Fatalf("pinned rc.common did not expose its empty-set failure semantic: %s", events)
		}
	})

	t.Run("restoration_failure_never_opens_an_instance", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		fixture.run(t, "/etc/init.d/solovey-ui start", map[string]string{"SUI_FAIL_STARTUP_RESTORE": "1"}, true)
		events := fixture.events(t)
		assertOrderedEvents(t, events, "prepare", "lifecycle:startup-restore")
		if strings.Contains(events, "instance:") {
			t.Fatalf("pinned rc.common opened procd state after restoration failure: %s", events)
		}
		if countEvent(events, "ubus:set") != 1 {
			t.Fatalf("pinned rc.common did not expose its empty-set failure semantic: %s", events)
		}
	})
}

type pinnedLifecycleRoot struct {
	root   string
	pinned string
}

func newPinnedLifecycleRoot(t *testing.T, pinned string) *pinnedLifecycleRoot {
	t.Helper()
	fixture := &pinnedLifecycleRoot{root: t.TempDir(), pinned: pinned}
	if err := os.MkdirAll(fixture.hostPath("/tmp/run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tmp", fixture.hostPath("/var")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/var/run", fixture.hostPath("/run")); err != nil {
		t.Fatal(err)
	}
	fixture.installBusyBox(t)
	fixture.writeMode(t, "/dev/null", nil, 0o666)
	fixture.installPinnedTree(t, "")
	fixture.installPackageMetadata(t, "")
	if err := os.MkdirAll(fixture.hostPath("/var/lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.write(t, "/bin/ubus", `#!/bin/sh
printf 'ubus:%s\n' "$3" >> "$SUI_PINNED_EVENTS"
[ "${SUI_FAIL_UBUS_METHOD:-}" != "$3" ]
`)
	fixture.write(t, "/bin/lock", "#!/bin/sh\nexit 0\n")
	fixture.write(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", `#!/bin/sh
echo prepare >> "$SUI_PINNED_EVENTS"
[ "${SUI_FAIL_PREPARE:-0}" != 1 ]
`)
	fixture.write(t, "/usr/lib/solovey-ui/solovey-openwrt-lifecycle", `#!/bin/sh
printf 'lifecycle:%s\n' "$1" >> "$SUI_PINNED_EVENTS"
case "$1" in
  reconcile)
    /etc/init.d/solovey-ui stop || exit 1
    /etc/init.d/solovey-ui start || exit 1
    ;;
	startup-restore)
	  [ "${SUI_FAIL_STARTUP_RESTORE:-0}" != 1 ] || exit 1
	  ;;
  pre-remove)
    [ "${SUI_FAIL_PRE_REMOVE:-0}" != 1 ] || exit 1
    ;;
  broker-entry|panel-entry)
    [ -e /etc/solovey-ui/prerequisites.ready ] || exit 1
    ;;
  *) exit 1 ;;
esac
`)
	fixture.write(t, "/etc/solovey-ui/prerequisites.ready", "ready\n")
	return fixture
}

func (f *pinnedLifecycleRoot) installBusyBox(t testing.TB) {
	t.Helper()
	busybox, err := exec.LookPath("busybox")
	if err != nil {
		t.Skip("busybox-static is required for the pinned OpenWrt chroot fixture")
	}
	data, err := os.ReadFile(busybox)
	if err != nil {
		t.Fatal(err)
	}
	f.writeMode(t, "/bin/busybox", data, 0o755)
	for _, name := range []string{"sh", "ash", "awk", "basename", "cat", "chmod", "chown", "cp", "cut", "date", "dd", "dirname", "echo", "find", "flock", "grep", "gzip", "hexdump", "ln", "ls", "mkdir", "mount", "readlink", "rm", "sed", "sleep", "sort", "tar", "touch", "tr", "wc"} {
		path := filepath.Join(f.root, "bin", name)
		if err := os.Symlink("busybox", path); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *pinnedLifecycleRoot) installPinnedTree(t testing.TB, prefix string) {
	t.Helper()
	files := map[string]string{
		"package/base-files/files/lib/functions.sh":         prefix + "/lib/functions.sh",
		"package/base-files/files/lib/functions/service.sh": prefix + "/lib/functions/service.sh",
		"package/system/procd/files/procd.sh":               prefix + "/lib/functions/procd.sh",
		"package/base-files/files/etc/rc.common":            prefix + "/etc/rc.common",
	}
	for source, target := range files {
		data, err := os.ReadFile(filepath.Join(f.pinned, filepath.FromSlash(source)))
		if err != nil {
			t.Fatal(err)
		}
		f.writeMode(t, target, data, 0o755)
	}
	init, err := os.ReadFile("solovey-ui.init")
	if err != nil {
		t.Fatal(err)
	}
	f.writeMode(t, prefix+"/etc/init.d/solovey-ui", init, 0o755)
	f.write(t, prefix+"/usr/share/libubox/jshn.sh", pinnedJSHNStub)
	if err := os.MkdirAll(f.hostPath(prefix+"/etc/rc.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.hostPath(prefix+"/var/lock"), 0o755); err != nil {
		t.Fatal(err)
	}
}

const pinnedJSHNStub = `
json_set_namespace() { [ "$#" -lt 2 ] || eval "$2=default"; return 0; }
json_init() { echo json:init >> "$SUI_PINNED_EVENTS"; }
json_add_string() { printf 'json:string:%s:%s\n' "$1" "$2" >> "$SUI_PINNED_EVENTS"; }
json_add_boolean() { printf 'json:boolean:%s:%s\n' "$1" "$2" >> "$SUI_PINNED_EVENTS"; }
json_add_int() { printf 'json:int:%s:%s\n' "$1" "$2" >> "$SUI_PINNED_EVENTS"; }
json_add_object() { printf 'json:object:%s\n' "$1" >> "$SUI_PINNED_EVENTS"; case "$1" in root-broker|panel) printf 'instance:%s\n' "$1" >> "$SUI_PINNED_EVENTS";; esac; }
json_close_object() { echo json:close-object >> "$SUI_PINNED_EVENTS"; }
json_add_array() { printf 'json:array:%s\n' "$1" >> "$SUI_PINNED_EVENTS"; }
json_close_array() { echo json:close-array >> "$SUI_PINNED_EVENTS"; }
json_select() { return 0; }
json_dump() { printf '{}'; }
json_cleanup() { :; }
json_get_values() { eval "$1="; }
`

func (f *pinnedLifecycleRoot) installPackageMetadata(t testing.TB, prefix string) {
	t.Helper()
	f.write(t, prefix+"/etc/passwd", "root:x:0:0:root:/root:/bin/ash\n")
	f.write(t, prefix+"/etc/group", "root:x:0:\n")
	f.write(t, prefix+"/etc/shadow", "root:*:0:0:99999:7:::\n")
	f.write(t, prefix+"/lib/apk/packages/solovey-ui.rusers", "solovey-ui:solovey-ui\n")
	f.write(t, prefix+"/lib/apk/packages/solovey-ui.list", "/etc/init.d/solovey-ui\n")
}

func (f *pinnedLifecycleRoot) postInstallScript(t *testing.T) string {
	t.Helper()
	return strings.Join([]string{
		`[ "${IPKG_NO_SCRIPT}" = "1" ] && exit 0`,
		`[ -s "${IPKG_INSTROOT}/lib/functions.sh" ] || exit 0`,
		`. "${IPKG_INSTROOT}/lib/functions.sh"`,
		`export root="${IPKG_INSTROOT}"`,
		`export pkgname="solovey-ui"`,
		`add_group_and_user`,
		`default_postinst`,
		packagePostInstallBody(t),
	}, "\n")
}

func (f *pinnedLifecycleRoot) preUpgradeScript(t testing.TB) string {
	t.Helper()
	body := definitionBody(t, pinnedSource(t, filepath.Join("..", ".."), "deploy/openwrt/package/solovey-ui/Makefile"), "Package/solovey-ui/preinst")
	return "export PKG_UPGRADE=1\n" + normalizeMakeShell(body)
}

func (f *pinnedLifecycleRoot) postUpgradeScript(t *testing.T) string {
	t.Helper()
	return "export PKG_UPGRADE=1\n" + f.postInstallScript(t)
}

func (f *pinnedLifecycleRoot) preDeinstallScript(t testing.TB) string {
	t.Helper()
	body := definitionBody(t, pinnedSource(t, filepath.Join("..", ".."), "deploy/openwrt/package/solovey-ui/Makefile"), "Package/solovey-ui/prerm")
	return strings.Join([]string{
		`[ -s "${IPKG_INSTROOT}/lib/functions.sh" ] || exit 0`,
		`. "${IPKG_INSTROOT}/lib/functions.sh"`,
		`export root="${IPKG_INSTROOT}"`,
		`export pkgname="solovey-ui"`,
		`default_prerm`,
		normalizeMakeShell(body),
	}, "\n")
}

func normalizeMakeShell(source string) string {
	return strings.ReplaceAll(strings.ReplaceAll(source, "$${", "${"), "$$", "$")
}

func (f *pinnedLifecycleRoot) run(t testing.TB, script string, extra map[string]string, success bool) {
	t.Helper()
	f.writeMode(t, "/case.sh", []byte("#!/bin/sh\n"+script+"\n"), 0o755)
	environment := []string{"PATH=/bin", "IPKG_INSTROOT=", "IPKG_NO_SCRIPT=", "PKG_UPGRADE=0", "SUI_PINNED_EVENTS=/events"}
	for key, value := range extra {
		environment = append(environment, key+"="+value)
	}
	command := exec.Command("chroot", f.root, "/bin/sh", "/case.sh")
	command.Env = environment
	output, err := command.CombinedOutput()
	if success && err != nil {
		t.Fatalf("pinned OpenWrt script failed: %v\n%s\nevents:\n%s", err, output, f.events(t))
	}
	if !success && err == nil {
		t.Fatalf("pinned OpenWrt script unexpectedly succeeded\nevents:\n%s", f.events(t))
	}
}

func (f *pinnedLifecycleRoot) assertServiceAccount(t testing.TB, prefix string) {
	t.Helper()
	passwd, err := os.ReadFile(f.hostPath(prefix + "/etc/passwd"))
	if err != nil {
		t.Fatal(err)
	}
	group, err := os.ReadFile(f.hostPath(prefix + "/etc/group"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(passwd), "solovey-ui:x:") || !strings.Contains(string(group), "solovey-ui:x:") {
		t.Fatalf("pinned add_group_and_user did not create the package identity: passwd=%q group=%q", passwd, group)
	}
}

func (f *pinnedLifecycleRoot) assertRespawnAndEntrypoints(t testing.TB, events string) {
	t.Helper()
	for _, fact := range []string{
		"json:string::/usr/lib/solovey-ui/solovey-openwrt-lifecycle",
		"json:string::broker-entry",
		"json:string::panel-entry",
		"json:string::3600",
		"json:string::3",
		"json:string::5",
	} {
		if !strings.Contains(events, fact) {
			t.Errorf("pinned procd message lacks %q: %s", fact, events)
		}
	}
}

func (f *pinnedLifecycleRoot) events(t testing.TB) string {
	t.Helper()
	data, err := os.ReadFile(f.hostPath("/events"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *pinnedLifecycleRoot) clearEvents(t testing.TB) {
	t.Helper()
	if err := os.WriteFile(f.hostPath("/events"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *pinnedLifecycleRoot) write(t testing.TB, name, value string) {
	t.Helper()
	f.writeMode(t, name, []byte(value), 0o755)
}

func (f *pinnedLifecycleRoot) writeMode(t testing.TB, name string, value []byte, mode os.FileMode) {
	t.Helper()
	path := f.hostPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, value, mode); err != nil {
		t.Fatal(err)
	}
}

func (f *pinnedLifecycleRoot) hostPath(name string) string {
	return filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
}

func pinnedOpenWrtSourceRoot(t testing.TB) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", "..", "..", "..", "upstreams", "openwrt-openwrt-25.12.5"))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Fatal("materialize canonical pinned OpenWrt references before qualification")
	} else if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func assertPinnedOpenWrtRevision(t testing.TB, root string) {
	t.Helper()
	output, err := exec.Command("git", "-c", "safe.directory="+root, "-C", root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(output)) != pinnedOpenWrtCommit {
		t.Fatalf("pinned OpenWrt source revision = %q, %v", strings.TrimSpace(string(output)), err)
	}
}

func assertOrderedEvents(t testing.TB, events string, required ...string) {
	t.Helper()
	position := 0
	for _, event := range required {
		index := strings.Index(events[position:], event)
		if index < 0 {
			t.Fatalf("event %q is absent or out of order after byte %d:\n%s", event, position, events)
		}
		position += index + len(event)
	}
}

func countEvent(events, event string) int {
	count := 0
	for _, line := range strings.Split(events, "\n") {
		if line == event {
			count++
		}
	}
	return count
}
