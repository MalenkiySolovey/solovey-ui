//go:build linux

package openwrt

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// This is real APK v3 registration/extraction/script execution on a Linux host.
// The existing pinned OpenWrt shell fixture models the external procd daemon;
// the durability subprocess runs production decisions over explicit mount facts.
// It does not claim physical F2FS/procd acceptance.
func TestAPKV3MixedRegistrationRecovery(t *testing.T) {
	apk := os.Getenv("SUI_APK_V3_PROGRAM")
	if apk == "" {
		t.Skip("set SUI_APK_V3_PROGRAM to the pinned SDK apk")
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated APK script chroot requires root")
	}
	version, err := exec.Command(apk, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "apk-tools 3.0.5") {
		t.Fatalf("apk authority: %s %v", version, err)
	}
	for _, scenario := range []string{"clean-install", "clean-upgrade", "mixed-r22-registration-r23-payload", "same-version-reinstall", "broken-r22-script-state"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPinnedLifecycleRoot(t, pinnedOpenWrtSourceRoot(t))
			f.write(t, "/etc/apk/arch", "x86_64\n")
			f.write(t, "/etc/apk/repositories", "")
			f.write(t, "/dev/null", "")
			if err := os.MkdirAll(f.hostPath("/tmp"), 0755); err != nil {
				t.Fatal(err)
			}
			// Chroot helper executable with its actual Linux shared libraries.
			executable, _ := os.Executable()
			copyFile := func(source, target string) {
				data, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				f.writeMode(t, target, data, 0755)
			}
			copyFile(executable, "/fixture-tests")
			libraries, _ := exec.Command("ldd", executable).CombinedOutput()
			for _, library := range regexp.MustCompile(`/[^\s()]+`).FindAllString(string(libraries), -1) {
				copyFile(library, library)
			}
			f.write(t, "/usr/lib/solovey-ui/solovey-openwrt-durability", "#!/bin/sh\nSUI_APK_DECISION_HELPER=1 /fixture-tests -test.run=^TestAPKV3ProductionDurabilityDecision$ -test.v || exit 1\necho durability:proven >> /events\n")
			prepare, err := os.ReadFile("solovey-openwrt-prepare")
			if err != nil {
				t.Fatal(err)
			}
			f.writeMode(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", prepare, 0755)
			for _, name := range []string{"id", "stat"} {
				if err := os.Symlink("busybox", f.hostPath("/bin/"+name)); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"owner", "broker"} {
				f.write(t, "/usr/lib/solovey-ui/solovey-openwrt-"+name+"-manifest", "#!/bin/sh\nexit 0\n")
			}
			f.write(t, "/usr/lib/solovey-ui/panel-payload", "candidate-panel\n")
			f.write(t, "/usr/lib/solovey-ui/broker-payload", "candidate-broker\n")
			work := t.TempDir()
			payload := filepath.Join(work, "payload")
			paths := []string{"/etc/init.d/solovey-ui", "/lib/apk/packages/solovey-ui.list", "/lib/apk/packages/solovey-ui.rusers", "/usr/lib/solovey-ui/solovey-openwrt-durability", "/usr/lib/solovey-ui/solovey-openwrt-prepare", "/usr/lib/solovey-ui/solovey-openwrt-owner-manifest", "/usr/lib/solovey-ui/solovey-openwrt-broker-manifest", "/usr/lib/solovey-ui/solovey-openwrt-lifecycle", "/usr/lib/solovey-ui/panel-payload", "/usr/lib/solovey-ui/broker-payload"}
			expected := map[string][32]byte{}
			for _, name := range paths {
				data, err := os.ReadFile(f.hostPath(name))
				if err != nil {
					t.Fatal(err)
				}
				dest := filepath.Join(payload, name)
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, data, 0755); err != nil {
					t.Fatal(err)
				}
				expected[name] = sha256.Sum256(data)
			}
			writeScript := func(name, body string) string {
				p := filepath.Join(work, name)
				if err := os.WriteFile(p, []byte("#!/bin/sh\nexport SUI_PINNED_EVENTS=/events\n"+body+"\n"), 0755); err != nil {
					t.Fatal(err)
				}
				return p
			}
			poison := writeScript("old-script", "echo OLD_SCRIPT_EXECUTED >> /events\nexit 77")
			pre := writeScript("new-pre", "echo candidate:pre >> /events\n"+f.preUpgradeScript(t))
			post := writeScript("new-post", f.postUpgradeScript(t)+"\nstatus=$?\n[ $status = 0 ] || exit $status\necho candidate:post-complete >> /events")
			oldPayload := payload
			if scenario == "broken-r22-script-state" {
				oldPayload = filepath.Join(work, "broken-r22-payload")
				if err := os.CopyFS(oldPayload, os.DirFS(payload)); err != nil {
					t.Fatal(err)
				}
				brokenInit := filepath.Join(oldPayload, "etc", "init.d", "solovey-ui")
				if err := os.WriteFile(brokenInit, []byte("#!/bin/sh /etc/rc.common\r\nexit 0\r\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			makePackage := func(version string, old bool) string {
				archive := filepath.Join(work, "solovey-ui-"+version+".apk")
				packagePayload := payload
				if old {
					packagePayload = oldPayload
				}
				args := []string{"mkpkg", "--info", "name:solovey-ui", "--info", "version:" + version, "--info", "arch:x86_64", "--files", packagePayload, "--output", archive}
				if old {
					for _, kind := range []string{"pre-upgrade", "post-upgrade", "pre-deinstall", "post-deinstall"} {
						args = append(args, "--script", kind+":"+poison)
					}
					if scenario == "broken-r22-script-state" {
						args = append(args, "--script", "post-install:"+poison)
					}
				} else {
					args = append(args, "--script", "pre-upgrade:"+pre, "--script", "post-upgrade:"+post, "--script", "post-install:"+post)
				}
				if out, err := exec.Command(apk, args...).CombinedOutput(); err != nil {
					t.Fatalf("mkpkg: %v %s", err, out)
				}
				return archive
			}
			old := makePackage("2026.3.1-r22", true)
			candidate := makePackage("2026.3.1-r23", false)
			if scenario == "same-version-reinstall" {
				old = candidate
			}
			runAPK := func(args ...string) string {
				all := append([]string{"--root", f.root, "--arch", "x86_64", "--allow-untrusted", "--repositories-file", "/dev/null"}, args...)
				out, err := exec.Command(apk, all...).CombinedOutput()
				if err != nil {
					t.Fatalf("apk %v: %v\n%s\nevents=%s", args, err, out, f.events(t))
				}
				return string(out)
			}
			if scenario == "broken-r22-script-state" {
				// Construct the already-observed physical boundary: r22 is registered,
				// its CRLF payload is unpacked, and APK records failed script state.
				// This is isolated fixture setup only; product recovery below is one
				// ordinary higher-release transaction with no database workaround.
				runAPK("add", "--initdb", "--no-scripts", old)
				setPackageScriptState(t, f.hostPath("/lib/apk/db/installed"), "solovey-ui", "f:S", "f:sS")
				assertPackageScriptState(t, f.hostPath("/lib/apk/db/installed"), "solovey-ui", "f:sS")
			} else if scenario != "clean-install" {
				runAPK("add", "--initdb", "--no-scripts", old)
			} else {
				runAPK("add", "--initdb")
			}
			preserved := map[string][]byte{"/etc/solovey-ui/db/database.db": []byte("opaque-existing-db-byte-fixture"), "/etc/solovey-ui/db/firewall-authority": []byte("committed-history-composition-trusted-management"), "/etc/config/firewall": []byte("existing-uci"), "/etc/solovey-ui/application-config": []byte("existing-config")}
			for name, data := range preserved {
				f.writeMode(t, name, data, 0600)
			}
			databasePath := f.hostPath("/etc/solovey-ui/db/solovey-ui.db")
			database, err := sql.Open("sqlite3", databasePath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec("CREATE TABLE preserved_authority (kind TEXT PRIMARY KEY, value TEXT); INSERT INTO preserved_authority VALUES ('operation','reconcile_required'),('composition','ACTIVE'),('contribution','baseline'),('trusted_management','retained'),('application','retained')"); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			preserved["/etc/solovey-ui/db/solovey-ui.db"], err = os.ReadFile(databasePath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "mixed-r22-registration-r23-payload" {
				for _, name := range paths {
					if strings.HasPrefix(name, "/usr/lib/") {
						data, _ := os.ReadFile(f.hostPath(name))
						f.writeMode(t, name, make([]byte, len(data)), 0755)
					}
				}
				f.write(t, "/usr/lib/solovey-ui/partial-r18-extra", "partial-new-payload")
			}
			f.clearEvents(t)
			transaction := []string{"add", "--initdb"}
			if scenario == "same-version-reinstall" {
				transaction = append(transaction, "--force-reinstall")
			}
			simulationArgs := append(append([]string{}, transaction...), "--simulate", candidate)
			if scenario == "broken-r22-script-state" {
				all := append([]string{"--root", f.root, "--arch", "x86_64", "--allow-untrusted", "--repositories-file", "/dev/null"}, simulationArgs...)
				out, err := exec.Command(apk, all...).CombinedOutput()
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !strings.Contains(string(out), "Upgrading solovey-ui (2026.3.1-r22 -> 2026.3.1-r23)") {
					t.Fatalf("broken-state simulation differential: %v\n%s", err, out)
				}
			} else {
				runAPK(simulationArgs...)
			}
			if f.events(t) != "" {
				t.Fatal("simulation ran hooks")
			}
			runAPK(append(transaction, candidate)...)
			installed := runAPK("list", "--installed", "solovey-ui")
			if !strings.Contains(installed, "2026.3.1-r23") {
				t.Fatalf("registration=%s", installed)
			}
			if scenario == "broken-r22-script-state" {
				assertPackageScriptState(t, f.hostPath("/lib/apk/db/installed"), "solovey-ui", "f:S")
			}
			for name, want := range expected {
				data, err := os.ReadFile(f.hostPath(name))
				if err != nil || sha256.Sum256(data) != want {
					t.Fatalf("payload incoherent %s: %v", name, err)
				}
			}
			for name, want := range preserved {
				data, err := os.ReadFile(f.hostPath(name))
				if err != nil || !bytes.Equal(data, want) {
					t.Fatalf("data changed %s: %v", name, err)
				}
			}
			database, err = sql.Open("sqlite3", "file:"+databasePath+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			var integrity string
			if err := database.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("preserved SQLite integrity: %s %v", integrity, err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			events := f.events(t)
			if strings.Contains(events, "OLD_SCRIPT_EXECUTED") {
				t.Fatal("registered old scripts were invoked")
			}
			assertOrderedEvents(t, events, "durability:proven", "instance:root-broker", "instance:panel", "candidate:post-complete")
			if scenario != "clean-install" {
				assertOrderedEvents(t, events, "candidate:pre", "ubus:delete", "durability:proven")
			}
			for _, line := range strings.Split(runAPK("audit", "--system"), "\n") {
				if strings.Contains(line, "usr/lib/solovey-ui/") && !strings.Contains(line, "partial-r18-extra") {
					t.Fatalf("package audit: %s", line)
				}
			}
			t.Log("real APK v3 registration/extraction/hook completion/data preservation PASS; procd and mount facts are explicit source fixtures")
		})
	}
}

func TestAPKV3ProductionDurabilityDecision(t *testing.T) {
	if os.Getenv("SUI_APK_DECISION_HELPER") != "1" {
		t.Skip("chroot fixture helper")
	}
	env := durabilityFixtureEnvironment(durabilityMounts, "OPAQUE")
	proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DatabaseDurabilityProofV3
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("proof round trip: %v", err)
	}
	if _, err := recheckDatabaseDurabilityProof(decoded, 1, env, func(string) (uint64, error) { return 4096, nil }); err != nil {
		t.Fatal(err)
	}
}

func assertPackageScriptState(t testing.TB, databasePath, packageName, expected string) {
	t.Helper()
	data, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	record, _, _ := packageDatabaseRecord(string(data), packageName)
	if record == "" {
		t.Fatalf("package %s is absent from %s", packageName, databasePath)
	}
	if !strings.Contains("\n"+record+"\n", "\n"+expected+"\n") {
		t.Fatalf("package %s state does not contain %s:\n%s", packageName, expected, record)
	}
}

func setPackageScriptState(t testing.TB, databasePath, packageName, oldState, newState string) {
	t.Helper()
	data, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	record, start, end := packageDatabaseRecord(text, packageName)
	if record == "" {
		t.Fatalf("package %s is absent from %s", packageName, databasePath)
	}
	oldLine := "\n" + oldState + "\n"
	withEnvelope := "\n" + record + "\n"
	if strings.Count(withEnvelope, oldLine) != 1 {
		t.Fatalf("package %s fixture state does not contain exactly one %s line:\n%s", packageName, oldState, record)
	}
	replacement := strings.TrimSuffix(strings.TrimPrefix(strings.Replace(withEnvelope, oldLine, "\n"+newState+"\n", 1), "\n"), "\n")
	if err := os.WriteFile(databasePath, []byte(text[:start]+replacement+text[end:]), 0644); err != nil {
		t.Fatal(err)
	}
}

func packageDatabaseRecord(database, packageName string) (string, int, int) {
	start := strings.Index(database, "P:"+packageName+"\n")
	if start < 0 {
		return "", -1, -1
	}
	end := len(database)
	if offset := strings.Index(database[start:], "\n\n"); offset >= 0 {
		end = start + offset
	}
	return database[start:end], start, end
}
