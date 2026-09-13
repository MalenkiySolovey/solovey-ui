//go:build linux

package privilegedbroker

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

func TestProductionReadinessChildNegotiatesAndExecsPanelMarker(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root broker and non-root readiness child are required")
	}
	source := os.Getenv("SUI_PRODUCTION_READINESS_BINARY")
	if source == "" {
		t.Skip("production readiness binary was not supplied by the Linux gate")
	}
	const (
		installRoot   = "/usr/lib/solovey-ui"
		readinessPath = installRoot + "/solovey-broker-readiness"
		panelPath     = installRoot + "/solovey-ui"
		peerUID       = 65534
		peerGID       = 65534
	)
	for _, path := range []string{installRoot, StandaloneSocketRoot} {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("fixed integration fixture path already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(installRoot) })
	copyExecutableFixture(t, source, readinessPath)
	if err := os.WriteFile(panelPath, []byte("#!/bin/sh\nprintf 'panel-exec' > \"$SUI_READINESS_MARKER\"\n"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(panelPath, 0o555); err != nil {
		t.Fatal(err)
	}
	object, err := executableobject.Open(readinessPath, executableobject.Policy{MaxBytes: 512 << 20, RequireRegular: true,
		RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022, RequireTrustedAncestry: true,
		AncestryOwner: 0, AncestryForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	identity := object.Identity()
	if err := object.Close(); err != nil {
		t.Fatal(err)
	}
	client := ClientManifest{Name: "broker-readiness", UID: peerUID, GID: peerGID, Executable: readinessPath,
		ExecutableDigest: identity.Digest, Device: identity.Device, Inode: identity.Inode, Roles: []Role{RolePanel},
		CgroupPolicy: CgroupOptional, CgroupAuthorityRevision: CgroupAuthorityRevisionV1,
		ProcdService: "solovey-ui", ProcdInstance: "panel", ProcdCommand: []string{readinessPath},
		ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui", ProcdRelation: ProcdRelationMain, CapabilitiesOnly: true}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaProcd, ApplicationOwnerRevision: Digest([]byte("readiness-owner")), Clients: []ClientManifest{client}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var peerPID atomic.Int64
	attestor.supervision = procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			pid := int(peerPID.Load())
			if pid <= 1 {
				return nil, errors.New("readiness PID is unavailable")
			}
			return procdRuntimeJSON(nil, client, pid), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			return procdCgroupEvidence{availability: processevidence.CgroupUnavailable, revision: Digest([]byte("optional-cgroup-absent"))}, nil
		},
	}
	registry := NewRegistry()
	var dispatched atomic.Int32
	if err := registry.Register(VerbCapabilities, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		dispatched.Add(1)
		return CapabilitiesV1{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
			Role: RolePanel, Verbs: []Verb{VerbCapabilities}, Revision: Digest([]byte("readiness-capabilities"))}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	boot, err := currentBootID()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry, memoryJournal{}, attestor, boot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(StandaloneSocketRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(StandaloneSocketRoot) })
	if err := os.Chown(StandaloneSocketRoot, 0, peerGID); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: DefaultSocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(DefaultSocketPath, 0o660); err != nil || os.Chown(DefaultSocketPath, 0, peerGID) != nil {
		t.Fatal("broker socket ownership setup failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()
	markerRoot, err := os.MkdirTemp("/run", "solovey-readiness-marker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(markerRoot) })
	if err := os.Chmod(markerRoot, 0o777); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(markerRoot, "panel-exec")
	launch := func() *exec.Cmd {
		command := exec.Command(readinessPath)
		command.Env = []string{"LANG=C", "LC_ALL=C", "SUI_READINESS_MARKER=" + marker}
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: peerUID, Gid: peerGID}}
		return command
	}
	command := launch()
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	peerPID.Store(int64(command.Process.Pid))
	if err := command.Wait(); err != nil {
		t.Fatalf("production readiness flow failed: %v: %s", err, output.String())
	}
	markerData, err := os.ReadFile(marker)
	if err != nil || string(markerData) != "panel-exec" || dispatched.Load() != 1 {
		t.Fatalf("marker=%q err=%v dispatched=%d", markerData, err, dispatched.Load())
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(readinessPath, readinessPath+".authorized"); err != nil {
		t.Fatal(err)
	}
	copyExecutableFixture(t, source, readinessPath)
	denied := launch()
	if err := denied.Start(); err != nil {
		t.Fatal(err)
	}
	peerPID.Store(int64(denied.Process.Pid))
	time.Sleep(750 * time.Millisecond)
	_ = denied.Process.Kill()
	_ = denied.Wait()
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) || dispatched.Load() != 1 {
		t.Fatalf("denied readiness exec marker err=%v dispatched=%d", err, dispatched.Load())
	}
	cancel()
	_ = listener.Close()
	server.ShutdownConnections()
	server.WaitConnections()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readinessPath, "test") {
		t.Fatal("production readiness path was replaced by a test-only identity")
	}
}
