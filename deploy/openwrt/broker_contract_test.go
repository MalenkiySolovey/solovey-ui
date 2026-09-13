package openwrt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestOpenWrtBrokerProjectionUsesOneFixedProcdPanelInstance(t *testing.T) {
	panel := ClientExecutableIdentity{Path: PanelExecutablePath, SHA256: strings.Repeat("a", 64), Device: 7, Inode: 11}
	readiness := ClientExecutableIdentity{Path: ReadinessExecutablePath, SHA256: strings.Repeat("b", 64), Device: 7, Inode: 12}
	proof := ClientExecutableIdentity{Path: SSHProofExecutablePath, SHA256: strings.Repeat("c", 64), Device: 7, Inode: 13}
	dropbear := ClientExecutableIdentity{Path: DropbearExecutablePath, SHA256: strings.Repeat("d", 64), Device: 7, Inode: 14}
	owner := brokerOwnerFixture(t, panel)
	manifest, err := ProcdBrokerManifest(owner, panel, readiness, proof, dropbear)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != broker.ManifestSchemaProcd || manifest.ApplicationOwnerRevision != owner.Revision || len(manifest.Clients) != 3 || manifest.Revision == "" {
		t.Fatalf("manifest projection = %#v", manifest)
	}
	for _, client := range manifest.Clients[:2] {
		if client.ProcdService != ProcdServiceName || client.ProcdInstance != ProcdPanelInstance ||
			client.ProcdRelation != broker.ProcdRelationMain || len(client.ProcdCommand) != 2 ||
			client.ProcdCommand[0] != LifecycleExecutablePath || client.ProcdCommand[1] != "panel-entry" ||
			len(client.Roles) != 1 || client.Roles[0] != broker.RolePanel {
			t.Fatalf("client projection = %#v", client)
		}
		if client.CapabilitiesOnly != (client.Name == "broker-readiness") {
			t.Fatalf("client capability scope = %#v", client)
		}
	}
	proofClient := manifest.Clients[2]
	if proofClient.Roles[0] != broker.RoleSSHProof || proofClient.ProcdService != "dropbear" || proofClient.ProcdInstance != "" || proofClient.ProcdInstanceSelector != broker.ProcdSelectorUniqueAncestor ||
		proofClient.ProcdRelation != broker.ProcdRelationAncestor || !proofClient.AnyNonRootUID || !proofClient.AnyGID || proofClient.RequiredGroup != owner.ProcessGID {
		t.Fatalf("proof projection = %#v", proofClient)
	}
	mainPath, proofPath, err := BrokerSocketPaths(DefaultProfile())
	if err != nil || mainPath != broker.DefaultSocketPath || proofPath != broker.ProofSocketPath {
		t.Fatalf("socket projection = %q %q, %v", mainPath, proofPath, err)
	}
}

func TestProcdInitTopologyKeepsRootBrokerAndUnprivilegedPanelSeparate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("solovey-ui.init"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		"USE_PROCD=1", "procd_open_instance root-broker", `solovey-openwrt-lifecycle" broker-entry`,
		"procd_open_instance panel", `solovey-openwrt-lifecycle" panel-entry`, "procd_set_param user solovey-ui",
		"procd_set_param group solovey-ui", "SUI_DB_FOLDER=/etc/solovey-ui/db", "procd_set_param respawn",
		`"$SOLOVEY_PREPARE" || return 1`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("procd init is missing %q", required)
		}
	}
	if strings.Count(content, "procd_open_instance") != 2 || strings.Contains(content, "systemctl") || strings.Contains(content, "uci ") || strings.Contains(content, "mkdir ") {
		t.Fatal("procd init topology contains an unsupported lifecycle surface")
	}
}

func TestOpenWrtBrokerProjectionRejectsUntrustedExecutableInputs(t *testing.T) {
	validPanel := ClientExecutableIdentity{Path: PanelExecutablePath, SHA256: strings.Repeat("a", 64), Device: 7, Inode: 11}
	validReadiness := ClientExecutableIdentity{Path: ReadinessExecutablePath, SHA256: strings.Repeat("b", 64), Device: 7, Inode: 12}
	validProof := ClientExecutableIdentity{Path: SSHProofExecutablePath, SHA256: strings.Repeat("c", 64), Device: 7, Inode: 13}
	validDropbear := ClientExecutableIdentity{Path: DropbearExecutablePath, SHA256: strings.Repeat("d", 64), Device: 7, Inode: 14}
	owner := brokerOwnerFixture(t, validPanel)
	for _, mutate := range []func(*ClientExecutableIdentity, *ClientExecutableIdentity){
		func(panel, _ *ClientExecutableIdentity) { panel.Path = "/tmp/panel" },
		func(_, readiness *ClientExecutableIdentity) { readiness.Path = "/tmp/readiness" },
		func(panel, _ *ClientExecutableIdentity) { panel.SHA256 = "bad" },
		func(_, readiness *ClientExecutableIdentity) { readiness.Inode = 0 },
	} {
		panel, readiness := validPanel, validReadiness
		mutate(&panel, &readiness)
		if _, err := ProcdBrokerManifest(owner, panel, readiness, validProof, validDropbear); err == nil {
			t.Fatal("unsafe OpenWrt broker projection was accepted")
		}
	}
	invalidProof := validProof
	invalidProof.Path = "/tmp/proof"
	if _, err := ProcdBrokerManifest(owner, validPanel, validReadiness, invalidProof, validDropbear); err == nil {
		t.Fatal("unsafe SSH proof executable was accepted")
	}
}

func TestOpenWrtBrokerWriterReaderRoundTripAndSecurityNegatives(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real reader security round-trip requires a root-owned filesystem fixture; run this gate in a root/user namespace")
	}
	panel := ClientExecutableIdentity{Path: PanelExecutablePath, SHA256: strings.Repeat("a", 64), Device: 7, Inode: 11}
	readiness := ClientExecutableIdentity{Path: ReadinessExecutablePath, SHA256: strings.Repeat("b", 64), Device: 7, Inode: 12}
	proof := ClientExecutableIdentity{Path: SSHProofExecutablePath, SHA256: strings.Repeat("c", 64), Device: 7, Inode: 13}
	dropbear := ClientExecutableIdentity{Path: DropbearExecutablePath, SHA256: strings.Repeat("d", 64), Device: 7, Inode: 14}
	manifest, err := ProcdBrokerManifest(brokerOwnerFixture(t, panel), panel, readiness, proof, dropbear)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if runtime.GOOS == "linux" {
		dir, err = os.MkdirTemp("/root", "solovey-broker-manifest-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	path := filepath.Join(dir, "broker-clients.json")
	writeBrokerFixture(t, path, manifest)
	loaded, err := broker.LoadManifest(path)
	if err != nil {
		t.Fatalf("real writer-shaped manifest was rejected by reader: %v", err)
	}
	if loaded.Revision != manifest.Revision || len(loaded.Clients) != len(manifest.Clients) || !loaded.RequiresProcd() {
		t.Fatalf("round-trip changed manifest: %#v", loaded)
	}

	negatives := map[string]func(string) error{
		"tampered mode":              func(path string) error { return os.Chmod(path, 0o660) },
		"tampered owner proposition": func(path string) error { return chownRootFixture(path, 1000, 1000) },
		"symlink": func(path string) error {
			target := path + ".target"
			if err := os.Rename(path, target); err != nil {
				return err
			}
			return os.Symlink(target, path)
		},
		"zero-size": func(path string) error { return os.Truncate(path, 0) },
		"oversize":  func(path string) error { return os.WriteFile(path, make([]byte, 256<<10+1), 0o640) },
		"invalid revision": func(path string) error {
			mutated := manifest
			mutated.Revision = "invalid"
			return writeBrokerBytes(path, mutated)
		},
		"invalid procd identity": func(path string) error {
			mutated := manifest
			mutated.Clients[0].ProcdCommand = nil
			mutated, err := broker.FinalizeManifest(mutated)
			if err != nil {
				return err
			}
			return writeBrokerBytes(path, mutated)
		},
	}
	for name, mutate := range negatives {
		t.Run(name, func(t *testing.T) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := writeBrokerBytes(path, manifest); err != nil {
				t.Fatal(err)
			}
			if err := mutate(path); err != nil {
				if name == "tampered owner proposition" {
					t.Skipf("owner mutation is unavailable in this user namespace: %v", err)
				}
				t.Fatal(err)
			}
			if name != "tampered owner proposition" {
				if err := chownRootFixture(path, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := broker.LoadManifest(path); err == nil {
				t.Fatalf("unsafe broker manifest accepted for %s", name)
			}
		})
	}
}

func writeBrokerFixture(t *testing.T, path string, manifest broker.Manifest) {
	t.Helper()
	if err := writeBrokerBytes(path, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := chownRootFixture(path, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func writeBrokerBytes(path string, manifest broker.Manifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o640)
}

func chownRootFixture(path string, uid, gid int) error {
	if err := os.Chown(path, uid, gid); err == nil {
		return nil
	}
	return exec.Command("sudo", "-n", "chown", fmt.Sprintf("%d:%d", uid, gid), path).Run()
}

func brokerOwnerFixture(t *testing.T, panel ClientExecutableIdentity) deploymentidentity.ApplicationOwnerContractProcdV1 {
	t.Helper()
	owner, err := deploymentidentity.NewProcdV1(
		"123e4567-e89b-12d3-a456-426614174000", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", ProcdServiceName, ProcdPanelInstance,
		panel.Path, panel.SHA256, 1001, 1001,
	)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
