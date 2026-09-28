//go:build linux

package serverprotection

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestBuiltBrokerProductionCompositionFirstDenialAuthorizationAndShutdown(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root broker process is required")
	}
	brokerSource := os.Getenv("SUI_PRODUCTION_BROKER_BINARY")
	readinessSource := os.Getenv("SUI_PRODUCTION_READINESS_BINARY")
	if brokerSource == "" || readinessSource == "" {
		t.Skip("production broker and readiness binaries were not supplied by the Linux gate")
	}
	const (
		installRoot   = "/usr/lib/solovey-ui"
		brokerPath    = installRoot + "/solovey-privileged-broker"
		readinessPath = installRoot + "/solovey-broker-readiness"
		panelPath     = installRoot + "/solovey-ui"
		manifestRoot  = "/etc/solovey-ui"
		journalRoot   = "/var/lib/solovey-ui-broker"
		runtimeRoot   = "/usr/local/solovey-ui"
		peerUID       = 65534
		peerGID       = 65534
	)
	for _, path := range []string{installRoot, manifestRoot, journalRoot, runtimeRoot, broker.StandaloneSocketRoot} {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("fixed broker-process fixture path already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(journalRoot) })
	for _, directory := range []struct {
		path string
		mode os.FileMode
	}{{installRoot, 0o755}, {manifestRoot, 0o750}, {broker.StandaloneSocketRoot, 0o750}} {
		if err := os.Mkdir(directory.path, directory.mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(directory.path) })
	}
	if err := os.Chown(broker.StandaloneSocketRoot, 0, peerGID); err != nil {
		t.Fatal(err)
	}
	copyBrokerProcessExecutable(t, brokerSource, brokerPath)
	copyBrokerProcessExecutable(t, readinessSource, readinessPath)
	if err := os.WriteFile(panelPath, []byte("#!/bin/sh\nprintf 'panel-exec' > \"$SUI_READINESS_MARKER\"\n"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(panelPath, 0o555); err != nil {
		t.Fatal(err)
	}
	panel, err := executableobject.Open(panelPath, executableobject.Policy{MaxBytes: 1 << 20, RequireRegular: true,
		RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022, RequireTrustedAncestry: true,
		AncestryOwner: 0, AncestryForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	panelDigest := panel.Identity().Digest
	_ = panel.Close()
	const instanceID = "00112233-4455-4677-8899-aabbccddeeff"
	sourceRevision := "src-" + broker.Digest([]byte("broker-process-source"))
	artifactRevision := "art-" + broker.Digest([]byte("broker-process-artifact"))
	deploymentID := "dep-" + broker.Digest([]byte("broker-process-deployment"))
	binding, err := protectionruntime.Bind(protectionruntime.Installed(), instanceID, sourceRevision, artifactRevision, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := deploymentidentity.NewSystemdV1(instanceID, sourceRevision, artifactRevision, deploymentID,
		binding.ContractRevision, binding.BindingRevision, "solovey-ui", "solovey-ui.service",
		"/etc/systemd/system/solovey-ui.service", broker.Digest([]byte("broker-process-unit")), "/init.scope",
		panelPath, panelDigest, peerUID, peerGID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(protectionruntime.Installed().RuntimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(protectionruntime.Installed().RuntimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	mountProof, err := protectionruntime.ObserveRuntimeMount(protectionruntime.Installed().RuntimeRoot, protectionruntime.RuntimeMountPersistent)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := protectionruntime.InstalledSystemdRuntimeRoot(owner, mountProof)
	if err != nil {
		t.Fatal(err)
	}
	ownerData, _ := json.Marshal(owner)
	runtimeData, _ := json.Marshal(runtime)
	if err := os.WriteFile(deploymentidentity.InstalledContractPath, append(ownerData, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protectionruntime.InstalledRuntimeRootPath, append(runtimeData, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	createdFacilities := make([]string, 0, 2)
	for _, facility := range []string{"/usr/sbin/nft", "/usr/sbin/sshd"} {
		if _, err := os.Lstat(facility); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(facility, []byte("#!/bin/sh\nexit 0\n"), 0o555); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(facility, 0o555); err != nil {
				t.Fatal(err)
			}
			createdFacilities = append(createdFacilities, facility)
		} else if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, facility := range createdFacilities {
			_ = os.Remove(facility)
		}
	})
	readiness, err := executableobject.Open(readinessPath, executableobject.Policy{MaxBytes: 512 << 20, RequireRegular: true,
		RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022, RequireTrustedAncestry: true,
		AncestryOwner: 0, AncestryForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	identity := readiness.Identity()
	if err := readiness.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{{
		Name: "broker-readiness", UID: peerUID, GID: peerGID, Executable: readinessPath,
		ExecutableDigest: identity.Digest, Device: identity.Device, Inode: identity.Inode,
		CgroupPolicy: broker.CgroupRequired, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1,
		CapabilitiesOnly: true, Roles: []broker.Role{broker.RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broker.DefaultManifest, append(manifestData, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(broker.DefaultManifest, 0o640); err != nil {
		t.Fatal(err)
	}
	mainListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: broker.DefaultSocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	proofListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: broker.ProofSocketPath, Net: "unix"})
	if err != nil {
		_ = mainListener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = mainListener.Close()
		_ = proofListener.Close()
	})
	for _, path := range []string{broker.DefaultSocketPath, broker.ProofSocketPath} {
		if err := os.Chmod(path, 0o660); err != nil || os.Chown(path, 0, peerGID) != nil {
			t.Fatal("activated socket ownership setup failed")
		}
	}
	mainFile, err := mainListener.File()
	if err != nil {
		t.Fatal(err)
	}
	proofFile, err := proofListener.File()
	if err != nil {
		t.Fatal(err)
	}
	arguments := "--transport=systemd-activated --ssh-implementation=openssh --ssh-service-control=systemd --ssh-log-evidence=journald --deployment-backend=systemd-native --update-mode=native-self-managed"
	wrapper := "LISTEN_PID=$$; export LISTEN_PID; exec \"$SUI_BROKER_BINARY\" " + arguments
	command := exec.Command("/bin/sh", "-c", wrapper)
	command.Env = []string{"LANG=C", "LC_ALL=C", "SUI_BROKER_BINARY=" + brokerPath, "LISTEN_FDS=2", "LISTEN_FDNAMES=main:proof"}
	command.ExtraFiles = []*os.File{mainFile, proofFile}
	var brokerOutput bytes.Buffer
	command.Stdout, command.Stderr = &brokerOutput, &brokerOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = mainFile.Close()
	_ = proofFile.Close()
	brokerExited := make(chan error, 1)
	go func() { brokerExited <- command.Wait() }()
	select {
	case err := <-brokerExited:
		t.Fatalf("built broker exited during production startup: %v: %s", err, brokerOutput.String())
	case <-time.After(250 * time.Millisecond):
	}
	markerRoot, err := os.MkdirTemp("/run", "solovey-broker-process-marker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(markerRoot) })
	if err := os.Chmod(markerRoot, 0o777); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(markerRoot, "panel-exec")
	launchReadiness := func() *exec.Cmd {
		child := exec.Command(readinessPath)
		child.Env = []string{"LANG=C", "LC_ALL=C", "SUI_READINESS_MARKER=" + marker}
		child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: peerUID, Gid: peerGID}}
		return child
	}
	if err := os.Rename(readinessPath, readinessPath+".authorized"); err != nil {
		t.Fatal(err)
	}
	copyBrokerProcessExecutable(t, readinessSource, readinessPath)
	denied := launchReadiness()
	if err := denied.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(750 * time.Millisecond)
	_ = denied.Process.Kill()
	_ = denied.Wait()
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first denied client executed the panel marker: %v", err)
	}
	if err := os.Remove(readinessPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(readinessPath+".authorized", readinessPath); err != nil {
		t.Fatal(err)
	}
	authorized := launchReadiness()
	var readinessOutput bytes.Buffer
	authorized.Stdout, authorized.Stderr = &readinessOutput, &readinessOutput
	if err := authorized.Run(); err != nil {
		t.Fatalf("authorized production client failed: %v: %s\nbroker: %s", err, readinessOutput.String(), brokerOutput.String())
	}
	markerData, err := os.ReadFile(marker)
	if err != nil || string(markerData) != "panel-exec" {
		t.Fatalf("authorized marker=%q err=%v", markerData, err)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-brokerExited:
		if err != nil {
			t.Fatalf("built broker did not terminate cleanly: %v: %s", err, brokerOutput.String())
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("built broker termination exceeded its bound")
	}
	_ = mainListener.Close()
	_ = proofListener.Close()
	if !bytes.Contains(brokerOutput.Bytes(), []byte("solovey-broker-audit")) {
		t.Fatalf("built broker emitted no closed audit evidence: %s", brokerOutput.String())
	}
}

func copyBrokerProcessExecutable(t *testing.T, source, target string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o555); err != nil {
		t.Fatal(err)
	}
}
