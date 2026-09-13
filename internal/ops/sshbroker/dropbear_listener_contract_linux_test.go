//go:build linux

package sshbroker

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	"golang.org/x/sys/unix"
)

func TestDropbearProjectionRejectsRealReusePortEndpointAmbiguity(t *testing.T) {
	first, second, port := openReusePortContractListeners(t)
	defer unix.Close(first)
	defer unix.Close(second)

	observation, err := listenerevidence.ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 2 {
		t.Fatalf("real kernel did not expose both same-tuple sockets: %#v", observation)
	}
	_, _, err = projectDropbearListenerAuthorities(
		observation.Sockets,
		uciSection{Internal: "section-A", Type: dropbearConfigName},
		dropbearInstanceEvidence{Name: "opaque-instance-A"},
		hostfacts.ProcessFact{},
		hostfacts.ServiceFact{},
		strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64),
		time.Unix(1_000, 0).UTC(),
	)
	if err == nil || err.Error() != "Dropbear listener endpoint identity is ambiguous" {
		t.Fatalf("same canonical Dropbear endpoint did not fail closed: %v", err)
	}
}

func openReusePortContractListeners(t *testing.T) (int, int, uint16) {
	t.Helper()
	create := func(port int) (int, uint16) {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.Bind(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.Listen(fd, 8); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		address, err := unix.Getsockname(fd)
		if err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		return fd, uint16(address.(*unix.SockaddrInet4).Port)
	}
	first, port := create(0)
	second, secondPort := create(int(port))
	if secondPort != port {
		unix.Close(first)
		unix.Close(second)
		t.Fatalf("reuse-port sockets bound different ports: %d != %d", port, secondPort)
	}
	return first, second, port
}
