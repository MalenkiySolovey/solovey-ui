//go:build linux

package privilegedbroker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

func TestCredentialFrameReportsTheActualWriter(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() { written <- WriteFrame(client, Request{ProtocolVersion: ProtocolVersion}, MaxRequestBytes) }()
	var request Request
	writer, err := readPeerRequest(server, &request, MaxRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if writer.PID != os.Getpid() || writer.UID != uint32(os.Geteuid()) || writer.GID != uint32(os.Getegid()) {
		t.Fatalf("writer credentials=%+v", writer)
	}
}

func TestCredentialFrameRejectsDescriptorTransfer(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	var frame bytes.Buffer
	if err := WriteFrame(&frame, Request{ProtocolVersion: ProtocolVersion}, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	descriptor, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer descriptor.Close()
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var sendErr error
	if err := raw.Write(func(fd uintptr) bool {
		_, sendErr = unix.SendmsgN(int(fd), frame.Bytes(), unix.UnixRights(int(descriptor.Fd())), nil, 0)
		return !errors.Is(sendErr, unix.EAGAIN)
	}); err != nil || sendErr != nil {
		t.Fatal(errors.Join(err, sendErr))
	}
	var request Request
	if _, err := readPeerRequest(server, &request, MaxRequestBytes); err == nil {
		t.Fatal("SCM_RIGHTS request unexpectedly accepted")
	}
}

func TestInheritedSocketWriterDiffersFromTheConnector(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	connector, err := socketPeerCredentials(server)
	if err != nil {
		t.Fatal(err)
	}
	file, err := client.File()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestInheritedSocketWriterHelper$")
	command.Env = append(os.Environ(), "SUI_INHERITED_SOCKET_WRITER=1")
	command.ExtraFiles = []*os.File{file}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	var request Request
	writer, readErr := readPeerRequest(server, &request, MaxRequestBytes)
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil {
		t.Fatalf("read=%v wait=%v output=%s", readErr, waitErr, output.String())
	}
	if connector.PID != os.Getpid() || writer.PID != command.Process.Pid || writer.PID == connector.PID {
		t.Fatalf("connector=%+v writer=%+v child=%d", connector, writer, command.Process.Pid)
	}
	identity := PeerIdentity{PID: connector.PID, UID: connector.UID, GID: connector.GID}
	if err := (ManifestAttestor{}).VerifyWriter(context.Background(), identity, writer); err == nil || peerAttestationClass(err) != PeerAttestationWriterMismatch {
		t.Fatalf("inherited writer error=%v class=%q", err, peerAttestationClass(err))
	}
}

func TestInheritedSocketWriterHelper(t *testing.T) {
	if os.Getenv("SUI_INHERITED_SOCKET_WRITER") != "1" {
		t.Skip("helper process only")
	}
	file := os.NewFile(3, "broker-socket")
	if file == nil {
		t.Fatal("inherited socket is absent")
	}
	defer file.Close()
	if err := WriteFrame(file, Request{ProtocolVersion: ProtocolVersion}, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
}

func TestPidfdLivenessFailsClosedAfterConnectorDeath(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestPidfdLivenessHelper$")
	command.Env = append(os.Environ(), "SUI_PIDFD_LIVENESS_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	identity := PeerIdentity{PID: command.Process.Pid, livenessFD: pidfd, hasLiveness: true}
	defer unix.Close(pidfd)
	if err := peerAlive(identity); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	deadline := time.Now().Add(time.Second)
	for peerAlive(identity) == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := peerAlive(identity); err == nil || peerAttestationClass(err) != PeerAttestationConnectorDeath {
		t.Fatalf("dead connector error=%v class=%q", err, peerAttestationClass(err))
	}
	// Even if the numeric PID field is made to name a currently live task, the
	// retained dead-task pidfd remains authoritative. Numeric reuse cannot turn
	// the original connector lease back into a live identity.
	identity.PID = os.Getpid()
	if err := peerAlive(identity); err == nil || peerAttestationClass(err) != PeerAttestationConnectorDeath {
		t.Fatalf("numeric PID substitution bypassed pidfd: %v", err)
	}
	if err := peerAlive(PeerIdentity{}); err == nil || peerAttestationClass(err) != PeerAttestationLivenessUnavailable {
		t.Fatalf("missing pidfd error=%v class=%q", err, peerAttestationClass(err))
	}
}

func TestPidfdLivenessHelper(t *testing.T) {
	if os.Getenv("SUI_PIDFD_LIVENESS_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(10 * time.Second)
}

func TestInheritedWriterIsDeniedBeforeDispatch(t *testing.T) {
	manifest, clientManifest := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	manifest.Clients[0].CapabilitiesOnly = false
	manifest, err := FinalizeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor.supervision = testProcdSupervision(clientManifest, os.Getpid(), "/services/solovey-ui/panel")
	registry := NewRegistry()
	var dispatched atomic.Int32
	if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		dispatched.Add(1)
		return struct{}{}, nil
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
	audits := make(chan AuditEvent, 4)
	server.Audit = func(event AuditEvent) { audits <- event }
	path := filepath.Join(t.TempDir(), "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		cancel()
		_ = listener.Close()
		t.Fatal(err)
	}
	defer client.Close()
	file, err := client.File()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestInheritedSocketRequestHelper$")
	command.Env = append(os.Environ(), "SUI_INHERITED_SOCKET_REQUEST=1", "SUI_INHERITED_SOCKET_BOOT="+boot)
	command.ExtraFiles = []*os.File{file}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("writer helper failed: %v: %s", err, output.String())
	}
	_ = client.SetDeadline(time.Now().Add(time.Second))
	var response Response
	if err := ReadFrame(client, &response, MaxResponseBytes); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != CodeUnauthorized || dispatched.Load() != 0 {
		t.Fatalf("response=%+v dispatched=%d", response, dispatched.Load())
	}
	select {
	case event := <-audits:
		if event.PeerAttestation != string(PeerAttestationWriterMismatch) {
			t.Fatalf("audit=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("writer denial audit was not emitted")
	}
	cancel()
	_ = listener.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTransferredSocketWriterIsDeniedBeforeDispatch(t *testing.T) {
	manifest, clientManifest := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	manifest.Clients[0].CapabilitiesOnly = false
	manifest, err := FinalizeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor.supervision = testProcdSupervision(clientManifest, os.Getpid(), "/services/solovey-ui/panel")
	registry := NewRegistry()
	var dispatched atomic.Int32
	if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		dispatched.Add(1)
		return struct{}{}, nil
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
	path := filepath.Join(t.TempDir(), "transferred-writer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	brokerFile, err := client.File()
	if err != nil {
		t.Fatal(err)
	}
	defer brokerFile.Close()
	controls, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parentControl := os.NewFile(uintptr(controls[0]), "transfer-parent")
	childControl := os.NewFile(uintptr(controls[1]), "transfer-child")
	defer parentControl.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestTransferredSocketWriterHelper$")
	command.Env = append(os.Environ(), "SUI_TRANSFERRED_SOCKET_WRITER=1", "SUI_TRANSFERRED_SOCKET_BOOT="+boot)
	command.ExtraFiles = []*os.File{childControl}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = childControl.Close()
	if _, err := unix.SendmsgN(int(parentControl.Fd()), []byte{1}, unix.UnixRights(int(brokerFile.Fd())), nil, 0); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("transferred writer helper failed: %v: %s", err, output.String())
	}
	if dispatched.Load() != 0 {
		t.Fatalf("transferred socket writer dispatched %d handlers", dispatched.Load())
	}
	cancel()
	_ = listener.Close()
	server.ShutdownConnections()
	server.WaitConnections()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTransferredSocketWriterHelper(t *testing.T) {
	if os.Getenv("SUI_TRANSFERRED_SOCKET_WRITER") != "1" {
		t.Skip("helper process only")
	}
	control := os.NewFile(3, "transfer-control")
	if control == nil {
		t.Fatal("transfer control socket is absent")
	}
	defer control.Close()
	payload := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, _, _, err := unix.Recvmsg(int(control.Fd()), payload, oob, 0)
	if err != nil || n != 1 {
		t.Fatalf("receive transferred socket: n=%d err=%v", n, err)
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(messages) != 1 {
		t.Fatalf("control messages=%d err=%v", len(messages), err)
	}
	descriptors, err := unix.ParseUnixRights(&messages[0])
	if err != nil || len(descriptors) != 1 {
		t.Fatalf("descriptors=%v err=%v", descriptors, err)
	}
	file := os.NewFile(uintptr(descriptors[0]), "transferred-broker-socket")
	if file == nil {
		t.Fatal("transferred broker socket is absent")
	}
	defer file.Close()
	requestPayload, digest, err := MarshalPayload(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		BootID: os.Getenv("SUI_TRANSFERRED_SOCKET_BOOT"), Role: RolePanel, Verb: VerbSSHObserve,
		RequestID: "request-transferred-writer", OperationID: "operation-transferred-writer",
		DeadlineAt: time.Now().Add(time.Minute).UnixMilli(), Payload: requestPayload, PayloadDigest: digest}
	if err := WriteFrame(file, request, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := ReadFrame(file, &response, MaxResponseBytes); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != CodeUnauthorized {
		t.Fatalf("transferred writer response=%+v", response)
	}
}

func TestInheritedSocketRequestHelper(t *testing.T) {
	if os.Getenv("SUI_INHERITED_SOCKET_REQUEST") != "1" {
		t.Skip("helper process only")
	}
	payload, digest, err := MarshalPayload(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		BootID: os.Getenv("SUI_INHERITED_SOCKET_BOOT"), Role: RolePanel, Verb: VerbSSHObserve,
		RequestID: "request-inherited-writer", OperationID: "operation-inherited-writer",
		DeadlineAt: time.Now().Add(time.Minute).UnixMilli(), Payload: payload, PayloadDigest: digest}
	file := os.NewFile(3, "broker-socket")
	if file == nil {
		t.Fatal("inherited socket is absent")
	}
	defer file.Close()
	if err := WriteFrame(file, request, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
}

type signalingManifestAttestor struct {
	ManifestAttestor
	once  *sync.Once
	ready chan<- struct{}
}

func (a signalingManifestAttestor) Attest(ctx context.Context, connection *net.UnixConn, role Role) (PeerIdentity, error) {
	peer, err := a.ManifestAttestor.Attest(ctx, connection, role)
	if err == nil {
		a.once.Do(func() { close(a.ready) })
	}
	return peer, err
}

func TestAuthorizedConnectorExecIsDeniedAtFinalDispatchBarrier(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned executable fixture is required")
	}
	manifest, clientManifest := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	manifest.Clients[0].CapabilitiesOnly = false
	manifest, err := FinalizeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	base, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var peerPID atomic.Int64
	base.supervision = procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			pid := int(peerPID.Load())
			if pid <= 1 {
				return nil, errors.New("test connector PID is unavailable")
			}
			return procdRuntimeJSON(nil, clientManifest, pid), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			return procdCgroupEvidence{path: "/services/solovey-ui/panel", availability: processevidence.CgroupAvailable, revision: strings.Repeat("c", 64)}, nil
		},
	}
	ready := make(chan struct{})
	attestor := signalingManifestAttestor{ManifestAttestor: base, once: &sync.Once{}, ready: ready}
	registry := NewRegistry()
	var dispatched atomic.Int32
	if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		dispatched.Add(1)
		return struct{}{}, nil
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
	socketPath := filepath.Join(t.TempDir(), "exec-transition.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()

	replacementRoot, err := os.MkdirTemp("/root", "solovey-exec-transition-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(replacementRoot) })
	replacement := filepath.Join(replacementRoot, "peer-replacement.test")
	copyExecutableFixture(t, os.Args[0], replacement)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlWrite.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestAuthorizedConnectorExecHelper$")
	command.Env = append(os.Environ(), "SUI_CONNECTOR_EXEC_HELPER=connect", "SUI_CONNECTOR_EXEC_SOCKET="+socketPath,
		"SUI_CONNECTOR_EXEC_BOOT="+boot, "SUI_CONNECTOR_EXEC_REPLACEMENT="+replacement)
	command.ExtraFiles = []*os.File{controlRead}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = controlRead.Close()
	peerPID.Store(int64(command.Process.Pid))
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal("initial connector attestation did not complete")
	}
	if _, err := controlWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("exec-transition helper failed: %v: %s", err, output.String())
	}
	if dispatched.Load() != 0 {
		t.Fatalf("exec-transition request dispatched %d handlers", dispatched.Load())
	}
	cancel()
	_ = listener.Close()
	server.ShutdownConnections()
	server.WaitConnections()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizedConnectorExecHelper(t *testing.T) {
	phase := os.Getenv("SUI_CONNECTOR_EXEC_HELPER")
	if phase == "" {
		t.Skip("helper process only")
	}
	if phase == "connect" {
		connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: os.Getenv("SUI_CONNECTOR_EXEC_SOCKET"), Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		file, err := connection.File()
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
			t.Fatal(err)
		}
		control := os.NewFile(3, "exec-control")
		if control == nil {
			t.Fatal("exec control descriptor is absent")
		}
		var release [1]byte
		if _, err := io.ReadFull(control, release[:]); err != nil {
			t.Fatal(err)
		}
		replacement := os.Getenv("SUI_CONNECTOR_EXEC_REPLACEMENT")
		environment := []string{
			"LANG=C", "LC_ALL=C", "SUI_CONNECTOR_EXEC_HELPER=write",
			"SUI_CONNECTOR_EXEC_SOCKET_FD=" + strconv.FormatUint(uint64(file.Fd()), 10),
			"SUI_CONNECTOR_EXEC_BOOT=" + os.Getenv("SUI_CONNECTOR_EXEC_BOOT"),
		}
		if err := unix.Exec(replacement, []string{replacement, "-test.run=^TestAuthorizedConnectorExecHelper$"}, environment); err != nil {
			t.Fatal(err)
		}
		return
	}
	if phase != "write" {
		t.Fatalf("unknown helper phase %q", phase)
	}
	descriptor, err := strconv.ParseUint(os.Getenv("SUI_CONNECTOR_EXEC_SOCKET_FD"), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(descriptor), "broker-socket")
	if file == nil {
		t.Fatal("broker socket descriptor is absent")
	}
	defer file.Close()
	payload, digest, err := MarshalPayload(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		BootID: os.Getenv("SUI_CONNECTOR_EXEC_BOOT"), Role: RolePanel, Verb: VerbSSHObserve,
		RequestID: "request-exec-transition", OperationID: "operation-exec-transition",
		DeadlineAt: time.Now().Add(time.Minute).UnixMilli(), Payload: payload, PayloadDigest: digest}
	if err := WriteFrame(file, request, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := ReadFrame(file, &response, MaxResponseBytes); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != CodeUnauthorized {
		t.Fatalf("exec-transition response=%+v", response)
	}
}

func copyExecutableFixture(t *testing.T, source, target string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func socketPeerCredentials(connection *net.UnixConn) (WriterCredentials, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return WriterCredentials{}, err
	}
	var credential *unix.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credential, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || socketErr != nil || credential == nil {
		if err == nil && socketErr == nil {
			return WriterCredentials{}, errors.New("socket peer credentials are unavailable")
		}
		return WriterCredentials{}, errors.Join(err, socketErr)
	}
	return WriterCredentials{PID: int(credential.Pid), UID: uint32(credential.Uid), GID: uint32(credential.Gid)}, nil
}
