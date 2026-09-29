//go:build linux

package privilegedbroker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func requireReceiveClass(t *testing.T, err error, want requestReceiveClass) {
	t.Helper()
	var failure *requestReceiveError
	if !errors.As(err, &failure) || failure.class != want {
		t.Fatalf("receive class: got %v, want %s", err, want)
	}
}

func TestCredentialFrameFailureClasses(t *testing.T) {
	frame := func(body string) []byte {
		result := make([]byte, 4+len(body))
		binary.BigEndian.PutUint32(result, uint32(len(body)))
		copy(result[4:], body)
		return result
	}
	for _, test := range []struct {
		name string
		data []byte
		want requestReceiveClass
	}{
		{"empty", nil, receiveEmpty},
		{"header_incomplete", []byte{0, 0}, frameIncomplete},
		{"body_incomplete", []byte{0, 0, 0, 5, '{'}, frameIncomplete},
		{"zero_length", []byte{0, 0, 0, 0}, frameLengthInvalid},
		{"oversized", []byte{0, 32, 0, 0}, frameOversized},
		{"multiple_frames", append(frame("{}"), frame("{}")...), frameMultiple},
		{"invalid_json", frame("{"), frameDecodeInvalid},
		{"unknown_member", frame(`{"secret":"not for diagnostics"}`), frameDecodeInvalid},
		{"duplicate_member", frame(`{"role":"panel","role":"panel"}`), frameDecodeInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, client := unixConnectionPair(t)
			defer server.Close()
			defer client.Close()
			if err := preparePeerConnection(server); err != nil {
				t.Fatal(err)
			}
			if len(test.data) > 0 {
				if _, err := client.Write(test.data); err != nil {
					t.Fatal(err)
				}
			}
			if err := client.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			var request Request
			_, err := readPeerRequest(server, &request, MaxRequestBytes)
			requireReceiveClass(t, err, test.want)
			owner, reason, stage, _, _ := err.(*requestReceiveError).BrokerDiagnostic()
			if owner != "broker_transport" || stage != "request_receive" || strings.Contains(reason, "secret") {
				t.Fatal("unbounded receive diagnostic")
			}
		})
	}
}

func TestCredentialFrameFragmentationAndQueuedCredentials(t *testing.T) {
	for _, prepareBeforeConnect := range []bool{false, true} {
		for _, chunk := range []int{1, 3, 4, 7, MaxRequestBytes} {
			t.Run(fmt.Sprintf("prepared_%t_chunk_%d", prepareBeforeConnect, chunk), func(t *testing.T) {
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: t.TempDir() + "/broker.sock", Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				if prepareBeforeConnect {
					if err := preparePeerListener(listener); err != nil {
						t.Fatal(err)
					}
				}
				client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				var frame bytes.Buffer
				if err := WriteFrame(&frame, Request{ProtocolVersion: ProtocolVersion}, MaxRequestBytes); err != nil {
					t.Fatal(err)
				}
				data := frame.Bytes()
				// Queue the complete split frame before accept or late SO_PASSCRED.
				for offset := 0; offset < len(data); offset += chunk {
					if _, err := client.Write(data[offset:min(offset+chunk, len(data))]); err != nil {
						t.Fatal(err)
					}
				}
				if err := preparePeerListener(listener); err != nil {
					t.Fatal(err)
				}
				server, err := listener.AcceptUnix()
				if err != nil {
					t.Fatal(err)
				}
				defer server.Close()
				if err := preparePeerConnection(server); err != nil {
					t.Fatal(err)
				}
				_ = server.SetReadDeadline(time.Now().Add(time.Second))
				var request Request
				writer, err := readPeerRequest(server, &request, MaxRequestBytes)
				if err != nil || writer.PID != os.Getpid() || request.ProtocolVersion != ProtocolVersion {
					t.Fatalf("queued fragmented request: writer=%+v error=%v", writer, err)
				}
			})
		}
	}
}

func TestCredentialControlFailureClasses(t *testing.T) {
	valid := unix.UnixCredentials(&unix.Ucred{Pid: int32(os.Getpid()), Uid: uint32(os.Getuid()), Gid: uint32(os.Getgid())})
	unsupported := append([]byte(nil), valid...)
	(*unix.Cmsghdr)(unsafe.Pointer(&unsupported[0])).Type = unix.SCM_SECURITY
	malformed := append([]byte(nil), valid...)
	(*unix.Cmsghdr)(unsafe.Pointer(&malformed[0])).SetLen(1)
	short := append([]byte(nil), valid[:unix.CmsgLen(0)]...)
	(*unix.Cmsghdr)(unsafe.Pointer(&short[0])).SetLen(unix.CmsgLen(0))
	for _, test := range []struct {
		name string
		oob  []byte
		want requestReceiveClass
	}{
		{"missing", nil, credentialsMissing},
		{"malformed_control", malformed, controlMalformed},
		{"unsupported", unsupported, controlUnsupported},
		{"invalid_pid", unix.UnixCredentials(&unix.Ucred{Pid: 0}), credentialsMalformed},
		{"short_credentials", short, credentialsMalformed},
		{"multiple", append(append([]byte(nil), valid...), valid...), credentialsAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := credentialsFromControl(test.oob)
			requireReceiveClass(t, err, test.want)
		})
	}
}

func TestTruncatedRightsCannotLeakDescriptors(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fds := make([]int, 40)
	for index := range fds {
		fds[index] = int(file.Fd())
	}
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.WriteMsgUnix([]byte{1}, unix.UnixRights(fds...), nil); err != nil {
		t.Fatal(err)
	}
	var request Request
	_, err = readPeerRequest(server, &request, MaxRequestBytes)
	requireReceiveClass(t, err, controlTruncated)
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("received descriptor leak: before=%d after=%d", len(before), len(after))
	}
}

func TestCredentialFrameRejectsWriterChangeWithinFrame(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte{0, 0, 16, 0}); err != nil {
		t.Fatal(err)
	}
	file, err := client.File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestInheritedSocketWriterHelper$")
	child.Env = append(os.Environ(), "SUI_INHERITED_SOCKET_WRITER=1")
	child.ExtraFiles = []*os.File{file}
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	var request Request
	_, err = readPeerRequest(server, &request, MaxRequestBytes)
	requireReceiveClass(t, err, writerChanged)
}

func TestCredentialFrameSpansReceiveBuffer(t *testing.T) {
	server, client := unixConnectionPair(t)
	defer server.Close()
	defer client.Close()
	if err := preparePeerConnection(server); err != nil {
		t.Fatal(err)
	}
	if err := client.SetWriteBuffer(4096); err != nil {
		t.Fatal(err)
	}
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = client.SetWriteDeadline(time.Now().Add(5 * time.Second))
	want := Request{Purpose: strings.Repeat("x", 256<<10)}
	written := make(chan error, 1)
	go func() { written <- WriteFrame(client, want, MaxRequestBytes) }()
	var request Request
	writer, err := readPeerRequest(server, &request, MaxRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if writer.PID != os.Getpid() || request.Purpose != want.Purpose {
		t.Fatal("fragmented frame lost authority or bytes")
	}
}

func TestReceiveFlagClassification(t *testing.T) {
	// MSG_TRUNC is not generated by normal Linux SOCK_STREAM reads. Exercise
	// the defensive flag branch directly, without claiming a kernel witness.
	_, err := receivedRequestCredentials(1, unix.MSG_TRUNC, nil, false)
	requireReceiveClass(t, err, frameTruncated)
	_, err = receivedRequestCredentials(1, 0, nil, false)
	requireReceiveClass(t, err, credentialsMissing)
}

func TestReceiveDiagnosticsAreClosedAndRetained(t *testing.T) {
	unsafe := requestReadFailure(requestReceiveClass("/private/payload"), errors.New("secret payload"))
	owner, reason, stage, errno, _ := unsafe.(*requestReceiveError).BrokerDiagnostic()
	if owner != "broker_transport" || reason != string(receiveFailed) || stage != "request_receive" || errno != "OTHER" {
		t.Fatal("receive diagnostics escaped the closed vocabulary")
	}
	ring, err := openRecentDiagnosticRingAt(diagnosticFixtureRoot(t), uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	err = ring.Record(DiagnosticEvent{Timestamp: time.Now(), Operation: "invalid", HandlerOwner: owner, HandlerReason: reason, HandlerStage: stage, HandlerErrno: errno})
	if err != nil {
		t.Fatal(err)
	}
	if len(ring.records) != 1 || ring.records[0].Reason != string(receiveFailed) {
		t.Fatal("receive failure missing from recent ring")
	}
}
