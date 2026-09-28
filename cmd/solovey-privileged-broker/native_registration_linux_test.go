//go:build linux && !minimal

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Execute all actual production registrars, using the existing child-chroot
// fixture pattern. No host install paths, services or firewall are mutated.
func TestNativeInstalledProductionHandlerGraph(t *testing.T) {
	if root := os.Getenv("SOLOVEY_NATIVE_GRAPH_ROOT"); root != "" {
		if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mount(root, root, "", syscall.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mount("/proc", filepath.Join(root, "proc"), "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Chroot(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir("/"); err != nil {
			t.Fatal(err)
		}
		runNativeInstalledGraph(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Fatal("root namespace executor required")
	}
	root := t.TempDir()
	for _, dir := range []string{"proc", "tmp", "etc/solovey-ui", "usr/bin", "usr/sbin", "var/lib/solovey-ui-broker", "usr/local/solovey-ui/.runtime/server-protection"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"systemctl", "systemd-analyze", "journalctl"} {
		data, err := os.ReadFile("/usr/bin/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "usr/bin", name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Registration attests an executable object but never executes sshd. This
	// fixture proves registration, not SSH daemon operation. nft is absent.
	data, err := os.ReadFile("/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr/sbin/sshd"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestNativeInstalledProductionHandlerGraph$", "-test.v")
	command.Env = append(os.Environ(), "SOLOVEY_NATIVE_GRAPH_ROOT="+root)
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	if testing.CoverMode() != "" {
		command.Args = append(command.Args, "-test.gocoverdir=/tmp")
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("production graph: %v\n%s", err, output)
	}
}

func runNativeInstalledGraph(t *testing.T) {
	root := protectionruntime.Installed().RuntimeRoot
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 987, 987); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(broker.DefaultJournalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	owner, err := protectionruntime.SystemdApplicationOwner(protectionruntime.SystemdApplicationOwnerInput{
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64),
		ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64),
		ServiceIdentity: "solovey-ui", SystemdUnit: "solovey-ui.service", ServiceFragmentPath: "/etc/systemd/system/solovey-ui.service",
		ServiceUnitSHA256: strings.Repeat("4", 64), ServiceControlGroup: "/system.slice/solovey-ui.service",
		ExecutablePath: "/usr/local/solovey-ui/releases/fixture/solovey-ui", ExecutableSHA256: strings.Repeat("5", 64), ProcessUID: 987, ProcessGID: 987,
	})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := protectionruntime.ObserveRuntimeMount(root, protectionruntime.RuntimeMountPersistent)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := protectionruntime.InstalledSystemdRuntimeRoot(owner, proof)
	if err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]any{deploymentidentity.InstalledContractPath: owner, protectionruntime.InstalledRuntimeRootPath: installed} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	previous, err := protectionruntime.LoadInstalledRuntimeRootAuthority()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount(root, root, "", syscall.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	if previous.Recheck() == nil {
		t.Fatal("existing process authority accepted a changed mount generation")
	}
	composition, err := parseRuntimeComposition([]string{"--transport=systemd-activated", "--ssh-implementation=openssh", "--ssh-service-control=systemd", "--ssh-log-evidence=journald", "--deployment-backend=systemd-native", "--update-mode=native-self-managed"})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := broker.OpenFileJournal(broker.DefaultJournalRoot, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	registry := broker.NewRegistry()
	if err := registerHandlerGraph(registry, composition, journal, productionHandlerRegistrars()); err != nil {
		t.Fatal(err)
	}
	verbs := registry.Verbs(broker.RolePanel)
	for _, verb := range []broker.Verb{"server-protection.capabilities", broker.VerbSSHObserve, broker.VerbDeploymentObserve, broker.VerbUpdateObserve} {
		if !slices.Contains(verbs, verb) {
			t.Fatalf("production graph lacks %s: %v", verb, verbs)
		}
	}
	if !slices.Contains(registry.Verbs(broker.RoleSSHProof), broker.VerbSSHProof) {
		t.Fatal("proof handler absent")
	}
	assertServerConstruction(t, registry)
	authority, err := protectionruntime.LoadInstalledRuntimeRootAuthority()
	if err != nil {
		t.Fatal(err)
	}
	managed, err := protectionhelper.NewManagedRoot(authority)
	if err != nil {
		t.Fatal(err)
	}
	engine := protectionhelper.NewContractEngineWithSSHComposition(managed, composition.ssh)
	capability := engine.Handle(protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion,
		Operation: protectionhelper.OperationCapabilities, Capabilities: &protectionhelper.CapabilitiesRequest{},
		Correlation: protectionhelper.Correlation{OperationID: "fixture-operation", InstanceID: owner.InstanceID}})
	if !capability.OK || capability.Capabilities == nil || capability.Capabilities.NFT.Available || capability.Capabilities.NFT.Reason == "" {
		t.Fatalf("missing optional nft must be bounded capability state: %+v", capability)
	}
	if err := os.Chmod(protectionruntime.InstalledRuntimeRootPath, 0o666); err != nil {
		t.Fatal(err)
	}
	err = registerHandlerGraph(broker.NewRegistry(), composition, journal, productionHandlerRegistrars())
	var diagnostic *broker.StartupDiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Reason != "runtime_root_load_failed" || errors.Unwrap(diagnostic) == nil {
		t.Fatalf("integrity failure did not remain bounded and fail closed: %v", err)
	}
}
