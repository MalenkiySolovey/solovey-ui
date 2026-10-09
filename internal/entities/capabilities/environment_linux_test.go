//go:build linux

package capabilities

import (
	"bufio"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestResolve1ObservationUsesPrivateBusWithoutClaimingName(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("private D-Bus fixture unavailable")
	}
	address := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(daemon, "--session", "--nofork", "--address="+address, "--print-address=1")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan bool, 1)
	go func() { ready <- bufio.NewScanner(output).Scan() }()
	select {
	case started := <-ready:
		if !started {
			t.Fatal("private daemon failed to start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("private daemon startup timed out")
	}
	available, state := observeResolve1At(address)
	if !available || state != Resolve1NameUnclaimed {
		t.Fatalf("private unclaimed observation: %v %s", available, state)
	}
	owner, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var present bool
	if err := owner.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, resolve1Name).Store(&present); err != nil || present {
		t.Fatal("read-only observer claimed the well-known name")
	}
	// The fixture bus is phase-owned; the real system bus is never mutated.
	reply, err := owner.RequestName(resolve1Name, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal("fixture could not own resolve1")
	}
	available, state = observeResolve1At(address)
	if !available || state != Resolve1NameCurrentProcess {
		t.Fatalf("owned observation: %v %s", available, state)
	}
	if reply, err := owner.ReleaseName(resolve1Name); err != nil || reply != dbus.ReleaseNameReplyReleased {
		t.Fatal("fixture lost ownership after observation")
	}
}

func TestResolve1ProbeBoundsSilentPeerAndRejectsRemoteAddress(t *testing.T) {
	for _, address := range []string{"tcp:host=127.0.0.1,port=1234", "unix:path=relative", "unix:path=/a,path=/b", "unix:path=/a;unix:path=/b", "unix:path=/a%00", "unix:abstract=a,path=/b"} {
		if _, ok := systemBusUnixEndpoint(address); ok {
			t.Fatal("unbounded or nonlocal address accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "silent")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	started := time.Now()
	available, state := observeResolve1At("unix:path=" + path)
	if available || state != Resolve1NameUnknown || time.Since(started) > 2*time.Second {
		t.Fatal("silent bus probe was not bounded/fail-closed")
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("silent fixture did not accept")
	}
}
