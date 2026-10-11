//go:build linux

package privilegedbroker

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const systemdRequestCount = 8

// The runner uses actual PID1 socket activation and separate nonroot client
// and constrained root broker units, including the production fixed paths.
func TestSystemdRequestBroker(t *testing.T) {
	unit := os.Getenv("SOLOVEY_TEST_REQUEST_CLIENT_UNIT")
	if unit == "" {
		t.Skip("requires the real-systemd request runner")
	}
	if os.Geteuid() != 0 {
		t.Fatal("broker must be root")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
		found := false
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, field) {
				continue
			}
			value, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, field)), 16, 64)
			if err != nil || value != 0x830cb {
				t.Fatalf("production capability contract changed: %s", line)
			}
			found = true
		}
		if !found {
			t.Fatalf("missing capability field %s", field)
		}
	}
	if !strings.Contains(string(status), "NoNewPrivs:\t1") {
		t.Fatal("NoNewPrivileges absent")
	}
	self, err := inspectCommonPeer(os.Getpid(), 0, 0, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 65534, GID: 65534, Executable: self.Executable,
		ExecutableDigest: self.ExecutableDigest, Device: self.Device, Inode: self.Inode,
		CgroupUnit: unit, CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := currentBootID()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(NewRegistry(), memoryJournal{}, attestor, boot)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	events := make(chan struct{}, systemdRequestCount)
	server.Diagnostic = func(event DiagnosticEvent) {
		fmt.Printf("REQUEST_DIAGNOSTIC phase=%s attestation=%s same_pid=%t same_uid=%t same_gid=%t\n",
			event.Phase, event.PeerAttestation, event.Writer.PID == event.Peer.PID,
			event.Writer.UID == event.Peer.UID, event.Writer.GID == event.Peer.GID)
		if event.HandlerReason != "" {
			fmt.Printf("REQUEST_DIAGNOSTIC class=%s errno=%s\n", event.HandlerReason, event.HandlerErrno)
		}
		events <- struct{}{}
	}
	server.Audit = func(event AuditEvent) {
		fmt.Printf("REQUEST_AUDIT phase=%s result=%s\n", event.Phase, event.ResultClass)
		if event.ResultClass == "success" {
			successes.Add(1)
			events <- struct{}{}
		}
	}
	listeners, err := OpenTransport(SystemdActivated)
	if err != nil {
		t.Fatal(err)
	}
	defer listeners.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listeners.Listeners[RolePanel], RolePanel) }()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for index := 0; index < systemdRequestCount; index++ {
		select {
		case err := <-done:
			t.Fatalf("serve stopped: %v", err)
		case <-events:
		case <-timer.C:
			t.Fatal("request events timed out")
		}
	}
	cancel()
	_ = listeners.Close()
	server.WaitConnections()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if successes.Load() != systemdRequestCount {
		t.Fatalf("CAPABILITY_EXCHANGE successes=%d want=%d", successes.Load(), systemdRequestCount)
	}
	t.Log("LIVE_CROSS_UID_ATTESTATION=PASS; REQUEST_FRAME_RECEIVE=PASS; WRITER_CREDENTIALS=PASS; FINAL_RECHECK=PASS; CAPABILITY_EXCHANGE=PASS; BROKER_CAP_KILL=ABSENT")
}

func TestSystemdRequestClient(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_REQUEST_CLIENT") != "1" {
		t.Skip("integration client only")
	}
	if os.Getuid() != 65534 || os.Getgid() != 65534 {
		t.Fatal("client must have the nonroot UID/GID")
	}
	client := NewClient(RolePanel)
	for index := 0; index < systemdRequestCount; index++ {
		var result CapabilitiesV1
		_, err := client.Invoke(context.Background(), Call{Verb: VerbCapabilities,
			OperationID: fmt.Sprintf("systemd-request-%d", index), Payload: struct{}{}, Timeout: 5 * time.Second}, &result)
		if err != nil {
			t.Errorf("production Client.Invoke: %v", err)
		} else if result.Role != RolePanel || result.ProtocolVersion != ProtocolVersion || len(result.Verbs) != 1 || result.Verbs[0] != VerbCapabilities {
			t.Error("invalid capability response")
		}
	}
	// Retain the connector while the broker observes EOF on client-side denial.
	// This is fixture lifetime control, not a transport retry or readiness fix.
	time.Sleep(2 * time.Second)
}
