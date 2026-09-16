//go:build linux

package openwrt

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The optional artifact input contains scripts extracted by apk adbdump from
// the actual SDK candidate. The ordinary source gate uses the existing pinned
// package-pack contract; both paths execute the unmodified pinned rc.common.
func bootPackageScripts(t *testing.T, f *pinnedLifecycleRoot) map[string]string {
	t.Helper()
	scripts := map[string]string{
		"post-install": f.postInstallScript(t), "pre-upgrade": f.preUpgradeScript(t),
		"post-upgrade": f.postUpgradeScript(t), "pre-deinstall": f.preDeinstallScript(t),
	}
	if filename := os.Getenv("SUI_BOOT_APK_SCRIPTS"); filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]string
		if err := json.Unmarshal(data, &actual); err != nil {
			t.Fatal(err)
		}
		for name := range scripts {
			body := actual[name]
			if body == "" || strings.Contains(body, "\r") {
				t.Fatalf("invalid actual APK script %s", name)
			}
			scripts[name] = body
		}
		t.Log("executing actual candidate APK scripts")
	}
	return scripts
}

func (f *pinnedLifecycleRoot) assertBootRegistration(t *testing.T, present bool) []os.FileInfo {
	t.Helper()
	init, err := os.ReadFile(f.hostPath("/etc/init.d/solovey-ui"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(string(init), "\n") {
		for key, prefix := range map[string]string{"START=": "S", "STOP=": "K"} {
			if strings.HasPrefix(line, key) {
				names = append(names, prefix+strings.TrimPrefix(line, key)+"solovey-ui")
			}
		}
	}
	entries, err := filepath.Glob(f.hostPath("/etc/rc.d/*solovey-ui"))
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		if len(entries) != 0 {
			t.Fatalf("unexpected boot registration: %v", entries)
		}
		return nil
	}
	if len(names) != 2 || len(entries) != len(names) {
		t.Fatalf("missing/duplicate registration: %v, wanted %v", entries, names)
	}
	var infos []os.FileInfo
	for _, name := range names {
		p := f.hostPath("/etc/rc.d/" + name)
		target, err := os.Readlink(p)
		if err != nil || target != "../init.d/solovey-ui" {
			t.Fatalf("registration %s: %q %v", name, target, err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatal("dangling registration", err)
		}
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	return infos
}

func TestAPKV3BootRegistrationLifecycle(t *testing.T) {
	apk := os.Getenv("SUI_APK_V3_PROGRAM")
	if apk == "" {
		t.Skip("set SUI_APK_V3_PROGRAM to pinned apk-tools 3.0.5")
	}
	if os.Geteuid() != 0 {
		t.Fatal("real APK chroot requires root")
	}
	version, err := exec.Command(apk, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "apk-tools 3.0.5") {
		t.Fatalf("apk authority: %s %v", version, err)
	}
	pinned := pinnedOpenWrtSourceRoot(t)
	assertPinnedOpenWrtRevision(t, pinned)
	for _, scenario := range []string{"clean-install", "enabled-upgrade", "broken-r22-through-r23", "current-r23-service-down", "same-version-replacement"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPinnedLifecycleRoot(t, pinned)
			f.write(t, "/etc/apk/arch", "x86_64\n")
			f.write(t, "/etc/apk/repositories", "")
			scripts := bootPackageScripts(t, f)
			work := t.TempDir()
			makePackage := func(release string, broken, historical bool) string {
				payload := filepath.Join(work, release+"-payload")
				for _, name := range []string{"/etc/init.d/solovey-ui", "/lib/apk/packages/solovey-ui.list", "/lib/apk/packages/solovey-ui.rusers"} {
					source := f.hostPath(name)
					if name == "/etc/init.d/solovey-ui" {
						source = "solovey-ui.init"
					}
					data, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					if name == "/etc/init.d/solovey-ui" {
						data, err = os.ReadFile("solovey-ui.init")
						if err != nil {
							t.Fatal(err)
						}
						data = append(data, []byte("\nprintf 'init:%s\\n' \"$action\" >> /events\n")...)
						if broken {
							data = []byte(strings.ReplaceAll(string(data), "\n", "\r\n"))
						}
					}
					p := filepath.Join(payload, name)
					if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, data, 0755); err != nil {
						t.Fatal(err)
					}
				}
				archive := filepath.Join(work, release+".apk")
				args := []string{"mkpkg", "--info", "name:solovey-ui", "--info", "version:2026.3.1-" + release, "--info", "arch:x86_64", "--files", payload, "--output", archive}
				for _, kind := range []string{"post-install", "pre-upgrade", "post-upgrade", "pre-deinstall"} {
					body := scripts[kind]
					if historical && (kind == "post-install" || kind == "post-upgrade") {
						// Exact r23 package-specific postinst, after the pinned platform prefix.
						prefix := strings.Split(f.postInstallScript(t), packagePostInstallBody(t))[0]
						body = prefix + "[ -n \"${IPKG_INSTROOT}\" ] && exit 0\n/usr/lib/solovey-ui/solovey-openwrt-lifecycle reconcile\n"
						if kind == "post-upgrade" {
							body = "export PKG_UPGRADE=1\n" + body
						}
					}
					p := filepath.Join(work, release+"-"+kind)
					if err := os.WriteFile(p, []byte("#!/bin/sh\nexport SUI_PINNED_EVENTS=/events\n"+body), 0755); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--script", kind+":"+p)
				}
				if out, err := exec.Command(apk, args...).CombinedOutput(); err != nil {
					t.Fatalf("mkpkg: %v %s", err, out)
				}
				return archive
			}
			runAPK := func(success bool, args ...string) string {
				all := append([]string{"--root", f.root, "--arch", "x86_64", "--allow-untrusted", "--repositories-file", "/dev/null"}, args...)
				out, err := exec.Command(apk, all...).CombinedOutput()
				if (err == nil) != success {
					t.Fatalf("apk %v: %v\n%s\nevents=%s", args, err, out, f.events(t))
				}
				return string(out)
			}
			candidate := makePackage("r24", false, false)
			// The fixture preloads init for shell-only tests. APK must own its
			// initial extraction; otherwise /etc protection creates .apk-new.
			if err := os.Remove(f.hostPath("/etc/init.d/solovey-ui")); err != nil {
				t.Fatal(err)
			}
			var before []os.FileInfo
			switch scenario {
			case "clean-install":
				runAPK(true, "add", "--initdb")
			case "broken-r22-through-r23":
				runAPK(false, "add", "--initdb", makePackage("r22", true, true))
				f.assertBootRegistration(t, false)
				runAPK(true, "add", "--upgrade", makePackage("r23", false, true))
				f.assertBootRegistration(t, false)
				if countEvent(f.events(t), "instance:panel") == 0 {
					t.Fatal("r23 defect witness did not start runtime")
				}
				t.Log("r22 CRLF install -> normal r23 upgrade: runtime started, boot registration ABSENT")
			case "current-r23-service-down":
				runAPK(true, "add", "--initdb", "--no-scripts", makePackage("r23", false, true))
				f.assertBootRegistration(t, false)
			case "enabled-upgrade":
				runAPK(true, "add", "--initdb", makePackage("r23", false, true))
				before = f.assertBootRegistration(t, true)
			case "same-version-replacement":
				runAPK(true, "add", "--initdb", candidate)
			}
			f.write(t, "/etc/solovey-ui/db/preserved", "durable-original-operation\n")
			f.clearEvents(t)
			args := []string{"add", "--upgrade"}
			if scenario == "same-version-replacement" {
				args = append(args, "--force-reinstall")
			}
			runAPK(true, append(args, candidate)...)
			after := f.assertBootRegistration(t, true)
			assertPackageScriptState(t, f.hostPath("/lib/apk/db/installed"), "solovey-ui", "f:S")
			if !strings.Contains(runAPK(true, "list", "--installed", "solovey-ui"), "2026.3.1-r24") {
				t.Fatal("candidate not registered")
			}
			events := f.events(t)
			assertOrderedEvents(t, events, "init:start", "lifecycle:startup-restore", "instance:panel", "lifecycle:reconcile")
			if before != nil {
				if countEvent(events, "init:enable") != 0 || countEvent(events, "init:disable") != 0 {
					t.Fatal("enabled upgrade churn", events)
				}
				for i := range before {
					if !os.SameFile(before[i], after[i]) {
						t.Fatal("enabled upgrade replaced registration inode")
					}
				}
			}
			f.clearEvents(t)
			// procd rcS enumerates S* and invokes each with 'boot'. This uses the
			// real resulting link and rc.common boot dispatch, not a procd model.
			f.run(t, `for init in /etc/rc.d/S*; do [ ! -x "$init" ] || "$init" boot; done`, nil, true)
			assertOrderedEvents(t, f.events(t), "init:boot", "lifecycle:startup-restore", "instance:root-broker", "instance:panel")
			if countEvent(f.events(t), "init:boot") != 1 {
				t.Fatal("duplicate boot invocation")
			}
			f.clearEvents(t)
			runAPK(true, "del", "solovey-ui")
			assertOrderedEvents(t, f.events(t), "init:disable", "init:stop", "lifecycle:pre-remove")
			links, _ := filepath.Glob(f.hostPath("/etc/rc.d/*solovey-ui"))
			if len(links) != 0 {
				t.Fatal("removal left stale links", links)
			}
			data, err := os.ReadFile(f.hostPath("/etc/solovey-ui/db/preserved"))
			if err != nil || string(data) != "durable-original-operation\n" {
				t.Fatal("package lifecycle changed durable state", err)
			}
		})
	}
}

func TestPackageBootRegistrationFailureStopsReconcile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chroot requires root")
	}
	f := newPinnedLifecycleRoot(t, pinnedOpenWrtSourceRoot(t))
	if err := os.Remove(f.hostPath("/etc/rc.d")); err != nil {
		t.Fatal(err)
	}
	f.write(t, "/etc/rc.d", "not-a-directory")
	f.run(t, packagePostInstallBody(t), nil, false)
	if strings.Contains(f.events(t), "lifecycle:reconcile") {
		t.Fatal("registration failure admitted reconciliation")
	}
}
