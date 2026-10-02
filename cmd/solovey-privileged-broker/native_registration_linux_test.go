//go:build linux && !minimal

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Execute all actual production registrars, using the existing child-chroot
// fixture pattern. No host install paths, services or firewall are mutated.
func TestNativeInstalledProductionHandlerGraph(t *testing.T) {
	runNativeInstalledProductionHandlerGraph(t)
}

func TestNativeColdBootProductionHandlerGraph(t *testing.T) {
	runNativeInstalledProductionHandlerGraph(t)
}

func runNativeInstalledProductionHandlerGraph(t *testing.T) {
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
	command := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
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
	coldBoot := t.Name() == "TestNativeColdBootProductionHandlerGraph"
	if coldBoot {
		// Persist the preboot allocation-tuning presentation captured on the
		// clean beta8 fixture. The real current mount is the same backing;
		// only this superblock option differs. Do not prewarm registration.
		if slices.Contains(proof.Mount.SuperOptions, "mb_optimize_scan=0") {
			t.Fatal("executor already has the preboot option")
		}
		fact := proof.Mount
		fact.SuperOptions = append(slices.Clone(fact.SuperOptions), "mb_optimize_scan=0")
		if err := fact.Seal(); err != nil {
			t.Fatal(err)
		}
		proof, err = protectionruntime.NewRuntimeMountProof(root, protectionruntime.RuntimeMountPersistent, fact)
		if err != nil {
			t.Fatal(err)
		}
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
	if !coldBoot {
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
	assertNativeGraphSockets(t, registry, journal)
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
	if coldBoot {
		// An actual replacement mount after successful cold binding must still
		// revoke the retained authority. This namespace belongs to the child.
		if err := syscall.Mount("tmpfs", root, "tmpfs", 0, "mode=0700"); err != nil {
			t.Fatal(err)
		}
		if authority.Recheck() == nil {
			t.Fatal("cold-bound authority accepted a substituted filesystem")
		}
		if _, err := protectionruntime.LoadInstalledRuntimeRootAuthority(); err == nil {
			t.Fatal("new process accepted substituted volatile backing")
		}
		if err := syscall.Unmount(root, 0); err != nil {
			t.Fatal(err)
		}
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

// The production graph and framed transports run in the isolated child process.
// Peer attestation is a fixture here; its real OS identity gates are covered by
// privilegedbroker's peer/process transport tests.
type nativeGraphAttestor struct{ startupAttestor }

func (nativeGraphAttestor) Attest(context.Context, *net.UnixConn, broker.Role) (broker.PeerIdentity, error) {
	return broker.PeerIdentity{BootID: "fixture-boot", Revision: broker.Digest([]byte("fixture-peer"))}, nil
}

func assertNativeGraphSockets(t *testing.T, registry *broker.Registry, journal broker.Journal) {
	t.Helper()
	server, err := broker.NewServer(registry, journal, nativeGraphAttestor{}, "fixture-boot")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := os.MkdirAll(broker.StandaloneSocketRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, role := range []broker.Role{broker.RolePanel, broker.RoleSSHProof} {
		path := broker.NewClient(role).SocketPath
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o660); err != nil {
			t.Fatal(err)
		}
		// Prepare credentials before clients race the Serve goroutine startup.
		raw, err := listener.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var optionErr error
		if err := raw.Control(func(fd uintptr) {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_PASSCRED, 1)
		}); err != nil || optionErr != nil {
			t.Fatalf("activate credential socket: %v %v", err, optionErr)
		}
		done := make(chan error, 1)
		go func() { done <- server.Serve(ctx, listener, role) }()
		t.Cleanup(func() {
			cancel()
			_ = listener.Close()
			server.ShutdownConnections()
			server.WaitConnections()
			if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		})
		for range 3 {
			client := broker.NewClient(role)
			client.SocketPath, client.BootID = path, "fixture-boot"
			var capabilities broker.CapabilitiesV1
			_, err := client.Invoke(ctx, broker.Call{Verb: broker.VerbCapabilities, OperationID: "socket-readiness", Timeout: time.Second, Payload: struct{}{}}, &capabilities)
			if role == broker.RolePanel {
				if err != nil || !slices.Contains(capabilities.Verbs, broker.VerbSSHObserve) || !slices.Contains(capabilities.Verbs, broker.VerbDeploymentObserve) || !slices.Contains(capabilities.Verbs, broker.VerbUpdateObserve) {
					t.Fatalf("main socket production capabilities: %v %+v", err, capabilities)
				}
			} else {
				// The proof socket must respond and enforce its narrower role.
				var failure *broker.PublicError
				if !errors.As(err, &failure) || failure.Code != broker.CodeInvalidRequest {
					t.Fatalf("proof socket role boundary: %v", err)
				}
			}
		}
	}
}
