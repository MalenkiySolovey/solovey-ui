//go:build linux

package openwrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

// Execute the actual OpenWrt APK post-install wrapper plus the package body
// against a small Unix filesystem model. Ownership/mode are recorded by
// fixture commands so the test does not require CAP_CHOWN, while wrapper
// ordering, preparation and procd gating remain real shell behavior.
func TestPackageHookCompleteLifecycleModel(t *testing.T) {
	hook := packageAPKPostInstallWrapper(t)
	cases := []struct {
		name        string
		preexisting bool
		wrong       bool
		runs        int
	}{
		{name: "FRESH_POSTINSTALL", runs: 1},
		{name: "REPEATED_POSTINSTALL", preexisting: true, runs: 2},
		{name: "POSTUPGRADE", preexisting: true, runs: 1},
		{name: "PREEXISTING_WRONG_PARENT", preexisting: true, wrong: true, runs: 1},
		{name: "PREEXISTING_CORRECT_PARENT", preexisting: true, runs: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newPackageHookModel(t)
			if tc.preexisting {
				model.prepareExisting(tc.wrong)
			}
			for run := 0; run < tc.runs; run++ {
				model.runAPKWrapper(t, hook)
			}
			model.assertComplete(t)
		})
	}
}

// TestFullOpenWrtPackageReadinessBoundary runs the same post-install wrapper
// through the real manifest writer composition, the privileged broker reader,
// and the generation/readiness checks represented by the package start fence.
// It is root-gated because the production reader intentionally requires a
// root-owned canonical manifest and ancestry.
func TestFullOpenWrtPackageReadinessBoundary(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("full OpenWrt package readiness fixture requires root")
	}
	model := newPackageReadinessModel(t)
	model.runAPKWrapper(t, packageAPKPostInstallWrapper(t))
	model.assertComplete(t)
	events := model.readEvents()
	for _, required := range []string{"prepare", "owner", "runtime-root-contract", "runtime-root-validated", "manifest-published", "manifest-loaded", "broker-schema-revision", "application-owner-revision", "procd-root-identity", "broker-ready", "panel-generation", "package-generation"} {
		if !strings.Contains(events, required) {
			t.Fatalf("full package readiness event %q missing: %s", required, events)
		}
	}
}

func TestPackageManifestWriterHelper(t *testing.T) {
	runtimeRoot := os.Getenv("SUI_MODEL_RUNTIME")
	if runtimeRoot == "" {
		t.Skip("package fixture helper only")
	}
	owner, _, err := loadPackageReadinessContracts()
	if err != nil {
		t.Fatal(err)
	}
	panel, readiness, proof, dropbear := packageReadinessClientIdentities()
	manifest, err := ProcdBrokerManifest(owner, panel, readiness, proof, dropbear)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runtimeRoot, "broker-clients.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	appendPackageFixtureEvent("manifest-published")
}

func TestPackageManifestReaderHelper(t *testing.T) {
	path := filepath.Join(os.Getenv("SUI_MODEL_RUNTIME"), "broker-clients.json")
	if os.Getenv("SUI_MODEL_RUNTIME") == "" {
		t.Skip("package fixture helper only")
	}
	owner, runtimeRoot, err := loadPackageReadinessContracts()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := broker.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != broker.ManifestSchemaProcd || manifest.ApplicationOwnerRevision != owner.Revision || len(manifest.Clients) != 3 {
		t.Fatalf("package broker manifest identity = %#v", manifest)
	}
	if runtimeRoot.OwnerContractRevision != owner.Revision ||
		runtimeRoot.RuntimeRootContractRevision != owner.RuntimeRootContractRevision ||
		runtimeRoot.RuntimeRootBindingRevision != owner.RuntimeRootBindingRevision ||
		runtimeRoot.InstanceID != owner.InstanceID || runtimeRoot.SourceRevision != owner.SourceRevision ||
		runtimeRoot.ArtifactRevision != owner.ArtifactRevision || runtimeRoot.DeploymentID != owner.DeploymentID {
		t.Fatal("materialized runtime-root contract is bound to a different application owner")
	}
	for _, client := range manifest.Clients {
		if client.ProcdService == "" || len(client.ProcdCommand) == 0 || client.CgroupAuthorityRevision != broker.CgroupAuthorityRevisionV1 {
			t.Fatalf("package broker client lacks procd identity: %#v", client)
		}
	}
	appendPackageFixtureEvent("manifest-loaded")
	appendPackageFixtureEvent("broker-schema-revision")
	appendPackageFixtureEvent("application-owner-revision")
	appendPackageFixtureEvent("procd-root-identity")
}

func TestPackageReadinessHelper(t *testing.T) {
	if os.Getenv("SUI_MODEL_RUNTIME") == "" {
		t.Skip("package fixture helper only")
	}
	evidence, err := packageReadinessEvidenceFixture()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := loadPackageReadinessContracts()
	if err != nil {
		t.Fatal(err)
	}
	evidence.Owner = owner
	path := filepath.Join(os.Getenv("SUI_MODEL_RUNTIME"), "broker-clients.json")
	manifest, err := broker.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Manifest = manifest
	if err := ValidatePackageGenerationReadiness(evidence); err != nil {
		t.Fatalf("production package-generation readiness boundary failed: %v", err)
	}
	appendPackageFixtureEvent("broker-ready")
	appendPackageFixtureEvent("panel-generation")
	appendPackageFixtureEvent("package-generation")
}

func appendPackageFixtureEvent(event string) {
	path := os.Getenv("SUI_MODEL_EVENTS")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(event + "\n")
}

type packageRuntimeRootFixture struct {
	OwnerContractRevision       string `json:"ownerContractRevision"`
	RuntimeRootContractRevision string `json:"runtimeRootContractRevision"`
	RuntimeRootBindingRevision  string `json:"runtimeRootBindingRevision"`
	InstanceID                  string `json:"instanceId"`
	SourceRevision              string `json:"sourceRevision"`
	ArtifactRevision            string `json:"artifactRevision"`
	DeploymentID                string `json:"deploymentId"`
}

func loadPackageReadinessContracts() (deploymentidentity.ApplicationOwnerContractProcdV1, packageRuntimeRootFixture, error) {
	parent := os.Getenv("SUI_MODEL_PARENT")
	owner, err := deploymentidentity.LoadProcdFromPath(filepath.Join(parent, "procd-application-owner-contract.json"))
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, packageRuntimeRootFixture{}, err
	}
	data, err := os.ReadFile(filepath.Join(parent, "server-protection-runtime-root.json"))
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, packageRuntimeRootFixture{}, err
	}
	var runtimeRoot packageRuntimeRootFixture
	if err := json.Unmarshal(data, &runtimeRoot); err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, packageRuntimeRootFixture{}, err
	}
	return owner, runtimeRoot, nil
}

func packageReadinessClientIdentities() (ClientExecutableIdentity, ClientExecutableIdentity, ClientExecutableIdentity, ClientExecutableIdentity) {
	panel := ClientExecutableIdentity{Path: PanelExecutablePath, SHA256: strings.Repeat("a", 64), Device: 7, Inode: 11}
	readiness := ClientExecutableIdentity{Path: ReadinessExecutablePath, SHA256: strings.Repeat("b", 64), Device: 7, Inode: 12}
	proof := ClientExecutableIdentity{Path: SSHProofExecutablePath, SHA256: strings.Repeat("c", 64), Device: 7, Inode: 13}
	dropbear := ClientExecutableIdentity{Path: DropbearExecutablePath, SHA256: strings.Repeat("d", 64), Device: 7, Inode: 14}
	return panel, readiness, proof, dropbear
}

func packageReadinessEvidenceFixture() (PackageGenerationEvidence, error) {
	panel, readiness, proof, dropbear := packageReadinessClientIdentities()
	owner, err := deploymentidentity.NewProcdV1(
		"123e4567-e89b-42d3-a456-426614174000", "src-"+strings.Repeat("1", 64), "art-"+panel.SHA256, "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", ProcdServiceName, ProcdPanelInstance,
		panel.Path, panel.SHA256, 32769, 32769,
	)
	if err != nil {
		return PackageGenerationEvidence{}, err
	}
	manifest, err := ProcdBrokerManifest(owner, panel, readiness, proof, dropbear)
	if err != nil {
		return PackageGenerationEvidence{}, err
	}
	panelObject := executableobject.Identity{Label: PanelExecutablePath, ResolvedPath: PanelExecutablePath, Device: panel.Device, Inode: panel.Inode,
		Size: 4096, Mode: 0o555, UID: 0, GID: 0, Digest: panel.SHA256}
	brokerObject := executableobject.Identity{Label: BrokerExecutablePath, ResolvedPath: BrokerExecutablePath, Device: 7, Inode: 21,
		Size: 4096, Mode: 0o555, UID: 0, GID: 0, Digest: strings.Repeat("e", 64)}
	return PackageGenerationEvidence{
		Owner: owner, Manifest: manifest,
		PanelInstance:  broker.ProcdInstanceEvidence{Name: ProcdPanelInstance, PID: 101, Command: []string{LifecycleExecutablePath, "panel-entry"}, User: PanelAccountName, Group: PanelAccountName},
		PanelProcess:   processevidence.Fact{PID: 101, Executable: PanelExecutablePath, ExeDigest: panelObject.Digest, ExeDevice: panelObject.Device, ExeInode: panelObject.Inode},
		PanelObject:    panelObject,
		BrokerInstance: broker.ProcdInstanceEvidence{Name: ProcdBrokerInstance, PID: 102, Command: []string{LifecycleExecutablePath, "broker-entry"}, User: "root", Group: "root"},
		BrokerProcess:  processevidence.Fact{PID: 102, Executable: BrokerExecutablePath, ExeDigest: brokerObject.Digest, ExeDevice: brokerObject.Device, ExeInode: brokerObject.Inode},
		BrokerObject:   brokerObject,
	}, nil
}

func TestPackageGenerationReadinessMatrix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PackageGenerationEvidence) error
		wantErr bool
	}{
		{name: "correct broker manifest"},
		{name: "stale manifest revision", wantErr: true, mutate: func(e *PackageGenerationEvidence) error {
			e.Manifest.Revision = strings.Repeat("f", 64)
			return nil
		}},
		{name: "application owner revision mismatch", wantErr: true, mutate: func(e *PackageGenerationEvidence) error {
			e.Manifest.ApplicationOwnerRevision = strings.Repeat("f", 64)
			var err error
			e.Manifest, err = broker.FinalizeManifest(e.Manifest)
			return err
		}},
		{name: "broker executable identity mismatch", wantErr: true, mutate: func(e *PackageGenerationEvidence) error {
			e.BrokerProcess.ExeDigest = strings.Repeat("f", 64)
			return nil
		}},
		{name: "panel executable generation mismatch", wantErr: true, mutate: func(e *PackageGenerationEvidence) error {
			e.PanelProcess.ExeInode++
			return nil
		}},
		{name: "stale package generation", wantErr: true, mutate: func(e *PackageGenerationEvidence) error {
			owner, err := deploymentidentity.NewProcdV1(e.Owner.InstanceID, e.Owner.SourceRevision, "art-"+strings.Repeat("f", 64),
				e.Owner.DeploymentID, e.Owner.RuntimeRootContractRevision, e.Owner.RuntimeRootBindingRevision, e.Owner.ServiceIdentity,
				e.Owner.ProcdService, e.Owner.ProcdInstance, e.Owner.ExecutablePath, e.Owner.ExecutableSHA256, e.Owner.ProcessUID, e.Owner.ProcessGID)
			if err != nil {
				return err
			}
			e.Owner = owner
			e.Manifest.ApplicationOwnerRevision = owner.Revision
			e.Manifest, err = broker.FinalizeManifest(e.Manifest)
			return err
		}},
		{name: "complete normal OpenWrt postinstall readiness"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := packageReadinessEvidenceFixture()
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				if err := test.mutate(&evidence); err != nil {
					t.Fatal(err)
				}
			}
			err = ValidatePackageGenerationReadiness(evidence)
			if test.wantErr && err == nil {
				t.Fatal("unsafe or stale package generation passed readiness")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("current package generation failed readiness: %v", err)
			}
		})
	}
}

func TestPackageAPKWrapperFailuresPreventProcdStart(t *testing.T) {
	for name, failingCommand := range map[string]string{
		"pre-start preparation failure": "#!/bin/sh\nexit 1",
		"startup restore failure":       "#!/bin/sh\nprintf 'restore-failed\\n' >> \"$SUI_MODEL_EVENTS\"\nexit 1",
		"manifest writer failure":       "#!/bin/sh\nexit 1",
		"manifest reader failure":       "#!/bin/sh\nexit 1",
	} {
		t.Run(name, func(t *testing.T) {
			model := newPackageHookModel(t)
			switch name {
			case "pre-start preparation failure":
				model.writeCommand(t, "prepare", failingCommand)
			case "startup restore failure":
				model.writeCommand(t, "lifecycle", failingCommand)
			case "manifest writer failure":
				model.writeCommand(t, "broker-writer", failingCommand)
			case "manifest reader failure":
				model.writeCommand(t, "reader-check", failingCommand)
			}
			model.runAPKWrapperExpectFailure(t, packageAPKPostInstallWrapper(t))
			events, err := os.ReadFile(model.events)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(events), "procd-root-broker") || strings.Contains(string(events), "procd-panel") {
				t.Fatalf("procd instance opened after %s: %s", name, events)
			}
		})
	}
}

func TestOpenWrtInitStartPreparesBeforeOpeningProcdInstances(t *testing.T) {
	initSource, err := os.ReadFile("solovey-ui.init")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                         string
		failPreparation, failRestore bool
	}{
		{name: "preparation and restoration succeed"},
		{name: "preparation fails", failPreparation: true},
		{name: "restoration fails", failRestore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.MkdirAll(filepath.Join(root, "run"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			events := filepath.Join(root, "events")
			prepare := filepath.Join(bin, "solovey-openwrt-prepare")
			prepareBody := "#!/bin/sh\nprintf 'prepare\\n' >> \"$SUI_INIT_EVENTS\"\n"
			if tc.failPreparation {
				prepareBody += "exit 1\n"
			}
			if err := os.WriteFile(prepare, []byte(prepareBody), 0o755); err != nil {
				t.Fatal(err)
			}
			lifecycle := filepath.Join(bin, "solovey-openwrt-lifecycle")
			lifecycleBody := "#!/bin/sh\nprintf 'restore\\n' >> \"$SUI_INIT_EVENTS\"\n[ \"$1\" = startup-restore ]\n"
			if tc.failRestore {
				lifecycleBody += "exit 1\n"
			}
			if err := os.WriteFile(lifecycle, []byte(lifecycleBody), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"chown", "chmod"} {
				body := "#!/bin/sh\nprintf '" + command + "\\n' >> \"$SUI_INIT_EVENTS\"\n"
				if err := os.WriteFile(filepath.Join(bin, command), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.ReplaceAll(string(initSource), "/usr/lib/solovey-ui", filepath.ToSlash(bin))
			script = strings.ReplaceAll(script, "/run/solovey-ui", filepath.ToSlash(filepath.Join(root, "run")))
			script += strings.Join([]string{
				"procd_open_instance() { printf 'open:%s\\n' \"$1\" >> \"$SUI_INIT_EVENTS\"; }",
				"procd_set_param() { :; }",
				"procd_close_instance() { :; }",
				"start_service",
			}, "\n")
			cmd := exec.Command("sh", "-eu", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SUI_INIT_EVENTS="+events)
			err := cmd.Run()
			if (tc.failPreparation || tc.failRestore) && err == nil {
				t.Fatal("init start unexpectedly succeeded after its startup fence failed")
			}
			if !tc.failPreparation && !tc.failRestore && err != nil {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(events)
			if readErr != nil {
				t.Fatal(readErr)
			}
			text := string(data)
			if tc.failPreparation || tc.failRestore {
				if strings.Contains(text, "open:root-broker") || strings.Contains(text, "open:panel") {
					t.Fatalf("procd opened despite preparation failure: %s", text)
				}
				return
			}
			prep := strings.Index(text, "prepare")
			restore := strings.Index(text, "restore")
			rootBroker := strings.Index(text, "open:root-broker")
			panel := strings.Index(text, "open:panel")
			if prep < 0 || restore < prep || rootBroker < restore || panel < rootBroker {
				t.Fatalf("init start ordering = %s", text)
			}
		})
	}
}

func TestOpenWrtInitFirewallTriggerAcceleratesPanelReconciliation(t *testing.T) {
	initSource, err := os.ReadFile("solovey-ui.init")
	if err != nil {
		t.Fatal(err)
	}
	script := string(initSource) + strings.Join([]string{
		"procd_add_reload_trigger() { printf 'trigger:%s\\n' \"$1\"; }",
		"procd_send_signal() { printf 'signal:%s:%s:%s\\n' \"$1\" \"$2\" \"$3\"; }",
		"service_triggers",
		"reload_service",
	}, "\n")
	output, err := exec.Command("sh", "-eu", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("OpenWrt firewall trigger fixture failed: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "trigger:firewall\nsignal:solovey-ui:panel:HUP" {
		t.Fatalf("OpenWrt firewall lifecycle projection = %q", got)
	}
}

type packageHookModel struct {
	root, bin, parent, db, runtime, events, metadata, functions string
	uid, gid                                                    string
	manifestReal                                                bool
}

func newPackageHookModel(t *testing.T) *packageHookModel {
	return newPackageHookModelWithOptions(t, false)
}

func newPackageReadinessModel(t *testing.T) *packageHookModel {
	return newPackageHookModelWithOptions(t, true)
}

func newPackageHookModelWithOptions(t *testing.T, manifestReal bool) *packageHookModel {
	t.Helper()
	tempParent := os.TempDir()
	if manifestReal && os.Geteuid() == 0 {
		tempParent = "/root"
	}
	root, err := os.MkdirTemp(tempParent, "solovey-package-hook-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	bin := filepath.Join(root, "model-bin")
	metadata := filepath.Join(root, "metadata")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(metadata, 0o755); err != nil {
		t.Fatal(err)
	}
	model := &packageHookModel{
		root: root, bin: bin,
		parent:  filepath.Join(root, "etc", "solovey-ui"),
		db:      filepath.Join(root, "etc", "solovey-ui", "db"),
		runtime: filepath.Join(root, "run", "solovey-ui"),
		events:  filepath.Join(metadata, "events"), metadata: metadata,
		uid: "32769", gid: "32769", manifestReal: manifestReal,
	}
	if manifestReal {
		if err := os.MkdirAll(filepath.Join(root, "tmp", "run"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "tmp"), filepath.Join(root, "var")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "var", "run"), filepath.Join(root, "run")); err != nil {
			t.Fatal(err)
		}
	}
	model.functions = filepath.Join(root, "functions.sh")
	model.writeFile(t, model.functions, strings.Join([]string{
		"#!/bin/sh",
		"add_group_and_user() { echo user-ready >> \"$SUI_MODEL_EVENTS\"; }",
		"default_postinst() { \"$SUI_MODEL_INIT\" enable; \"$SUI_MODEL_INIT\" start; }",
	}, "\n"), 0o755)
	model.writeCommand(t, "id", strings.Join([]string{
		"#!/bin/sh",
		"case \"$1:$2\" in",
		"  -u:solovey-ui) printf '%s\\n' \"$SUI_MODEL_UID\" >> \"$SUI_MODEL_EVENTS\"; printf '%s\\n' \"$SUI_MODEL_UID\" ;;",
		"  -g:solovey-ui) printf '%s\\n' \"$SUI_MODEL_GID\" >> \"$SUI_MODEL_EVENTS\"; printf '%s\\n' \"$SUI_MODEL_GID\" ;;",
		"  *) exit 1 ;;",
		"esac",
	}, "\n"))
	model.writeCommand(t, "chown", strings.Join([]string{
		"#!/bin/sh", "set -eu",
		"owner=\"$1\"; path=\"$2\"; key=$(printf '%s' \"$path\" | tr '/' '_')",
		"case \"$owner\" in",
		"  root:root) printf '0:0\\n' > \"$SUI_MODEL_META/$key.uidgid\" ;;",
		"  root:solovey-ui) printf '0:%s\\n' \"$SUI_MODEL_GID\" > \"$SUI_MODEL_META/$key.uidgid\" ;;",
		"  solovey-ui:solovey-ui) printf '%s:%s\\n' \"$SUI_MODEL_UID\" \"$SUI_MODEL_GID\" > \"$SUI_MODEL_META/$key.uidgid\" ;;",
		"  *) exit 1 ;;",
		"esac",
		"printf 'chown:%s:%s\\n' \"$owner\" \"$path\" >> \"$SUI_MODEL_EVENTS\"",
	}, "\n"))
	model.writeCommand(t, "chmod", strings.Join([]string{
		"#!/bin/sh", "set -eu",
		"mode=\"$1\"; path=\"$2\"; key=$(printf '%s' \"$path\" | tr '/' '_')",
		"printf '%s\\n' \"$mode\" > \"$SUI_MODEL_META/$key.mode\"",
		"printf 'chmod:%s:%s\\n' \"$mode\" \"$path\" >> \"$SUI_MODEL_EVENTS\"",
	}, "\n"))
	ownerWriter := []string{
		"#!/bin/sh", "set -eu",
		"key=$(printf '%s' \"$SUI_MODEL_PARENT\" | tr '/' '_')",
		"[ \"$(cat \"$SUI_MODEL_META/$key.uidgid\")\" = 0:0 ]",
		"[ \"$(cat \"$SUI_MODEL_META/$key.mode\")\" = 0711 ]",
		"printf 'owner\\n' >> \"$SUI_MODEL_EVENTS\"",
		"manifest=\"$SUI_MODEL_PARENT/procd-application-owner-contract.json\"; printf 'root-only-owner\\n' > \"$manifest\"; mkey=$(printf '%s' \"$manifest\" | tr '/' '_'); printf '0:0\\n' > \"$SUI_MODEL_META/$mkey.uidgid\"; printf '0444\\n' > \"$SUI_MODEL_META/$mkey.mode\"",
	}
	if manifestReal {
		ownerWriter = []string{
			"#!/bin/sh", "set -eu",
			"key=$(printf '%s' \"$SUI_MODEL_PARENT\" | tr '/' '_')",
			"[ \"$(cat \"$SUI_MODEL_META/$key.uidgid\")\" = 0:0 ]",
			"[ \"$(cat \"$SUI_MODEL_META/$key.mode\")\" = 0711 ]",
			"\"$SUI_MODEL_TEST_BINARY\" -test.run=^TestPackageOwnerRuntimeWriterHelper$ -test.v >/dev/null",
		}
	}
	model.writeCommand(t, "owner-writer", strings.Join(ownerWriter, "\n"))
	brokerWriter := []string{
		"#!/bin/sh", "set -eu",
		"key=$(printf '%s' \"$SUI_MODEL_RUNTIME\" | tr '/' '_')",
		"[ \"$(cat \"$SUI_MODEL_META/$key.uidgid\")\" = 0:$SUI_MODEL_GID ]",
		"[ \"$(cat \"$SUI_MODEL_META/$key.mode\")\" = 0750 ]",
		"printf 'broker\\n' >> \"$SUI_MODEL_EVENTS\"",
	}
	if manifestReal {
		brokerWriter = append(brokerWriter,
			"manifest=\"$SUI_MODEL_RUNTIME/broker-clients.json\"; \"$SUI_MODEL_TEST_BINARY\" -test.run=^TestPackageManifestWriterHelper$ -test.v >/dev/null",
		)
	} else {
		brokerWriter = append(brokerWriter,
			"manifest=\"$SUI_MODEL_RUNTIME/broker-clients.json\"; printf '{\"schema\":2,\"clients\":[{\"name\":\"panel\"}]}\\n' > \"$manifest\"; mkey=$(printf '%s' \"$manifest\" | tr '/' '_'); printf '0:0\\n' > \"$SUI_MODEL_META/$mkey.uidgid\"; printf '0640\\n' > \"$SUI_MODEL_META/$mkey.mode\"",
		)
	}
	model.writeCommand(t, "broker-writer", strings.Join(brokerWriter, "\n"))
	model.writeCommand(t, "prepare", strings.Join([]string{
		"#!/bin/sh", "set -eu", "printf 'prepare\\n' >> \"$SUI_MODEL_EVENTS\"",
		"id -u solovey-ui >/dev/null; id -g solovey-ui >/dev/null",
		"if [ -e \"$SUI_MODEL_PARENT\" ]; then [ -d \"$SUI_MODEL_PARENT\" ]; else mkdir -m 0711 \"$SUI_MODEL_PARENT\"; fi",
		"mkdir -p \"$SUI_MODEL_PARENT/db\"",
		"mkdir -p \"$SUI_MODEL_RUNTIME\"",
		"chown root:root \"$SUI_MODEL_PARENT\"; chmod 0711 \"$SUI_MODEL_PARENT\"",
		"chown solovey-ui:solovey-ui \"$SUI_MODEL_PARENT/db\"; chmod 0700 \"$SUI_MODEL_PARENT/db\"",
		"chown root:solovey-ui \"$SUI_MODEL_RUNTIME\"; chmod 0750 \"$SUI_MODEL_RUNTIME\"",
		"\"$SUI_MODEL_BIN/owner-writer\"", "\"$SUI_MODEL_BIN/broker-writer\"",
	}, "\n"))
	readerCheck := []string{"#!/bin/sh", "set -eu", "manifest=\"$SUI_MODEL_RUNTIME/broker-clients.json\""}
	if manifestReal {
		readerCheck = append(readerCheck,
			"SUI_MODEL_MANIFEST=\"$manifest\" \"$SUI_MODEL_TEST_BINARY\" -test.run=^TestPackageManifestReaderHelper$ -test.v >/dev/null",
			"\"$SUI_MODEL_TEST_BINARY\" -test.run=^TestPackageRuntimeContractReaderHelper$ -test.v >/dev/null",
		)
	} else {
		readerCheck = append(readerCheck, "grep -q '^\\{\"schema\":2' \"$manifest\"")
	}
	model.writeCommand(t, "reader-check", strings.Join(readerCheck, "\n"))
	model.writeCommand(t, "lifecycle", strings.Join([]string{
		"#!/bin/sh", "set -eu",
		"case \"$1\" in",
		"  startup-restore) printf 'restore\\n' >> \"$SUI_MODEL_EVENTS\" ;;",
		"  reconcile) \"$SUI_MODEL_INIT\" stop; \"$SUI_MODEL_INIT\" start; printf 'generation-current\\n' >> \"$SUI_MODEL_EVENTS\" ;;",
		"  *) exit 1 ;;",
		"esac",
	}, "\n"))
	init := filepath.Join(root, "etc", "init.d", "solovey-ui")
	startLine := `  start) "$SUI_MODEL_BIN/prepare" && "$SUI_MODEL_BIN/lifecycle" startup-restore && "$SUI_MODEL_BIN/reader-check" && echo procd-root-broker >> "$SUI_MODEL_EVENTS" && echo procd-panel >> "$SUI_MODEL_EVENTS" ;;`
	if manifestReal {
		startLine = `  start) "$SUI_MODEL_BIN/prepare" && "$SUI_MODEL_BIN/lifecycle" startup-restore && "$SUI_MODEL_BIN/reader-check" && "$SUI_MODEL_TEST_BINARY" -test.run=^TestPackageReadinessHelper$ -test.v >/dev/null && echo procd-root-broker >> "$SUI_MODEL_EVENTS" && echo procd-panel >> "$SUI_MODEL_EVENTS" ;;`
	}
	if err := os.MkdirAll(filepath.Dir(init), 0o755); err != nil {
		t.Fatal(err)
	}
	model.writeFile(t, init, strings.Join([]string{
		"#!/bin/sh",
		"case \"$1\" in",
		"  enable) echo enable >> \"$SUI_MODEL_EVENTS\" ;;",
		startLine,
		"  stop) echo stop >> \"$SUI_MODEL_EVENTS\" ;;",
		"  *) exit 1 ;;",
		"esac",
	}, "\n"), 0o755)
	return model
}

func (m *packageHookModel) writeCommand(t *testing.T, name, body string) {
	t.Helper()
	m.writeFile(t, filepath.Join(m.bin, name), body, 0o755)
}

func (m *packageHookModel) writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body+"\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func (m *packageHookModel) prepareExisting(wrong bool) {
	if err := os.MkdirAll(m.db, 0o755); err != nil {
		panic(err)
	}
	parentKey := strings.NewReplacer("/", "_").Replace(m.parent)
	dbKey := strings.NewReplacer("/", "_").Replace(m.db)
	parentOwner, parentMode := "0:0", "0711"
	dbOwner, dbMode := m.uid+":"+m.gid, "0700"
	if wrong {
		parentOwner, parentMode = "1000:1000", "0755"
		dbOwner, dbMode = "1000:1000", "0755"
	}
	m.writeMeta(parentKey, parentOwner, parentMode)
	m.writeMeta(dbKey, dbOwner, dbMode)
}

func (m *packageHookModel) writeMeta(key, owner, mode string) {
	if err := os.WriteFile(filepath.Join(m.metadata, key+".uidgid"), []byte(owner+"\n"), 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(m.metadata, key+".mode"), []byte(mode+"\n"), 0o600); err != nil {
		panic(err)
	}
}

func (m *packageHookModel) runAPKWrapper(t *testing.T, hook string) {
	t.Helper()
	if err := m.runAPKWrapperCommand(hook); err != nil {
		events, _ := os.ReadFile(m.events)
		t.Fatalf("package hook failed: %v\n%s", err, events)
	}
}

func (m *packageHookModel) runAPKWrapperExpectFailure(t *testing.T, hook string) {
	t.Helper()
	if err := m.runAPKWrapperCommand(hook); err == nil {
		t.Fatal("package wrapper unexpectedly succeeded after injected failure")
	}
}

func (m *packageHookModel) runAPKWrapperCommand(hook string) error {
	root := filepath.ToSlash(m.root)
	// Replace the nested paths through a marker so the parent substitution
	// cannot rewrite the path that was just expanded for the database.
	const parentMarker = "__SUI_MODEL_PARENT__"
	hook = strings.ReplaceAll(hook, "/etc/solovey-ui/db", parentMarker+"/db")
	hook = strings.ReplaceAll(hook, "/etc/solovey-ui", parentMarker)
	hook = strings.ReplaceAll(hook, parentMarker, root+"/etc/solovey-ui")
	hook = strings.ReplaceAll(hook, "/usr/lib/solovey-ui/solovey-openwrt-owner-manifest", root+"/model-bin/owner-writer")
	hook = strings.ReplaceAll(hook, "/usr/lib/solovey-ui/solovey-openwrt-broker-manifest", root+"/model-bin/broker-writer")
	hook = strings.ReplaceAll(hook, "/usr/lib/solovey-ui/solovey-openwrt-prepare", root+"/model-bin/prepare")
	hook = strings.ReplaceAll(hook, "/usr/lib/solovey-ui/solovey-openwrt-lifecycle", root+"/model-bin/lifecycle")
	hook = strings.ReplaceAll(hook, "/etc/init.d/solovey-ui", root+"/etc/init.d/solovey-ui")
	hook = strings.ReplaceAll(hook, ". ${IPKG_INSTROOT}/lib/functions.sh", ". \"$SUI_MODEL_FUNCTIONS\"")
	hook = strings.ReplaceAll(hook, "[ -s ${IPKG_INSTROOT}/lib/functions.sh ]", "[ -s \"$SUI_MODEL_FUNCTIONS\" ]")
	cmd := exec.Command("sh", "-eu", "-c", hook)
	cmd.Env = append(os.Environ(),
		"PATH="+m.bin+":"+os.Getenv("PATH"),
		"IPKG_NO_SCRIPT=",
		"IPKG_INSTROOT=",
		"SUI_MODEL_UID="+m.uid, "SUI_MODEL_GID="+m.gid,
		"SUI_MODEL_PARENT="+root+"/etc/solovey-ui",
		"SUI_MODEL_RUNTIME="+root+"/run/solovey-ui",
		"SUI_MODEL_META="+m.metadata, "SUI_MODEL_EVENTS="+m.events,
		"SUI_MODEL_FUNCTIONS="+m.functions, "SUI_MODEL_INIT="+root+"/etc/init.d/solovey-ui",
		"SUI_MODEL_BIN="+m.bin, "SUI_MODEL_TEST_BINARY="+os.Args[0],
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if output, err := cmd.Output(); err != nil {
		return fmt.Errorf("exit_status=%d stage=%s error=%v stderr=%s stdout=%s", cmd.ProcessState.ExitCode(), lifecycleStage(m.readEvents()), err, stderr.String(), output)
	}
	return nil
}

func (m *packageHookModel) readEvents() string {
	data, _ := os.ReadFile(m.events)
	return string(data)
}

func lifecycleStage(events string) string {
	stage := "none"
	for _, candidate := range []string{"owner", "broker", "enable", "start"} {
		if strings.Contains(events, candidate) {
			stage = candidate
		}
	}
	return stage
}

func (m *packageHookModel) assertComplete(t *testing.T) {
	t.Helper()
	parentKey := strings.NewReplacer("/", "_").Replace(m.parent)
	dbKey := strings.NewReplacer("/", "_").Replace(m.db)
	readMeta := func(key, suffix string) string {
		data, err := os.ReadFile(filepath.Join(m.metadata, key+suffix))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	if got := readMeta(parentKey, ".uidgid"); got != "0:0" {
		t.Fatalf("persistent parent owner=%s", got)
	}
	if got := readMeta(parentKey, ".mode"); got != "0711" {
		t.Fatalf("persistent parent mode=%s", got)
	}
	if got := readMeta(dbKey, ".uidgid"); got != m.uid+":"+m.gid {
		t.Fatalf("database owner=%s", got)
	}
	if got := readMeta(dbKey, ".mode"); got != "0700" {
		t.Fatalf("database mode=%s", got)
	}
	runtimeKey := strings.NewReplacer("/", "_").Replace(m.runtime)
	if got := readMeta(runtimeKey, ".uidgid"); got != "0:"+m.gid || readMeta(runtimeKey, ".mode") != "0750" {
		t.Fatalf("runtime parent authority owner=%s mode=%s", got, readMeta(runtimeKey, ".mode"))
	}
	items := []struct{ root, file, mode string }{
		{m.parent, "procd-application-owner-contract.json", "0444"},
		{m.runtime, "broker-clients.json", "0640"},
	}
	if m.manifestReal {
		items = append(items, struct{ root, file, mode string }{m.parent, "server-protection-runtime-root.json", "0444"})
	}
	for _, item := range items {
		path := filepath.Join(item.root, item.file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("manifest writer did not complete: %s: %v", item.file, err)
		}
		if m.manifestReal {
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			expectedMode, parseErr := strconv.ParseUint(item.mode, 8, 32)
			if parseErr != nil || !ok || !info.Mode().IsRegular() || info.Mode().Perm() != os.FileMode(expectedMode) || stat.Uid != 0 || stat.Gid != 0 {
				t.Fatalf("real package contract protection is invalid for %s: info=%v stat=%v parse=%v", item.file, info, stat, parseErr)
			}
			continue
		}
		key := strings.NewReplacer("/", "_").Replace(path)
		if got := readMeta(key, ".uidgid"); got != "0:0" || readMeta(key, ".mode") != item.mode {
			t.Fatalf("root-only manifest content is not protected: %s owner=%s mode=%s", item.file, got, readMeta(key, ".mode"))
		}
	}
	events, err := os.ReadFile(m.events)
	if err != nil {
		t.Fatal(err)
	}
	text := string(events)
	requiredEvents := []string{"user-ready", "prepare", "owner", "broker", "restore", "enable", "stop", "generation-current", "procd-root-broker", "procd-panel"}
	if m.manifestReal {
		requiredEvents = append(requiredEvents, "runtime-root-contract", "runtime-root-validated")
	}
	for _, required := range requiredEvents {
		if !strings.Contains(text, required) {
			t.Fatalf("lifecycle event %q missing: %s", required, text)
		}
	}
	if strings.Index(text, "enable") > strings.Index(text, "prepare") || strings.Index(text, "prepare") > strings.Index(text, "owner") || strings.Index(text, "owner") > strings.Index(text, "broker") || strings.Index(text, "broker") > strings.Index(text, "restore") || strings.Index(text, "restore") > strings.Index(text, "procd-root-broker") {
		t.Fatalf("package hook order is wrong: %s", text)
	}
	uidIndex := strings.Index(text, m.uid)
	gidIndex := strings.Index(text, m.gid)
	firstChown := strings.Index(text, "chown:")
	if uidIndex < 0 || gidIndex < 0 || firstChown < uidIndex || firstChown < gidIndex {
		t.Fatalf("user/group availability was not established before ownership changes: %s", text)
	}
	parentMode, _ := strconv.ParseUint(readMeta(parentKey, ".mode"), 8, 32)
	dbMode, _ := strconv.ParseUint(readMeta(dbKey, ".mode"), 8, 32)
	if parentMode&1 == 0 || parentMode&4 != 0 || dbMode != 0700 {
		t.Fatalf("service traversal/database privacy proposition failed: parent=%o db=%o", parentMode, dbMode)
	}
	if _, err := os.Stat(filepath.Join(m.runtime, "broker-clients.json")); err != nil {
		t.Fatalf("root-only content missing: %v", err)
	}
}

func packagePostInstallBody(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("package", "solovey-ui", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start, end := -1, -1
	for index, line := range lines {
		if line == "define Package/solovey-ui/postinst" {
			start = index + 1
		}
		if start >= 0 && line == "endef" {
			end = index
			break
		}
	}
	if start < 0 || end < start {
		t.Fatal("package post-install body is missing")
	}
	// The recipe escapes shell dollar signs for make; normalize that escape so
	// the extracted body is the script apk will actually execute.
	body := strings.ReplaceAll(strings.Join(lines[start:end], "\n")+"\n", "$${", "${")
	if strings.Contains(body, `\n`) {
		t.Fatal("package post-install body contains a literal backslash-n")
	}
	return body
}

// packageAPKPostInstallWrapper reconstructs the wrapper emitted by
// OpenWrt 25.12 include/package-pack.mk for CONFIG_USE_APK. The fixture
// substitutes only the absolute functions.sh path; add_group_and_user and
// default_postinst remain the live-root ordering authority.
func packageAPKPostInstallWrapper(t *testing.T) string {
	t.Helper()
	return strings.Join([]string{
		"#!/bin/sh",
		"[ \"${IPKG_NO_SCRIPT}\" = \"1\" ] && exit 0",
		"[ -s ${IPKG_INSTROOT}/lib/functions.sh ] || exit 0",
		". ${IPKG_INSTROOT}/lib/functions.sh",
		"export root=\"${IPKG_INSTROOT}\"",
		"export pkgname=\"solovey-ui\"",
		"add_group_and_user",
		"default_postinst",
		packagePostInstallBody(t),
	}, "\n")
}
