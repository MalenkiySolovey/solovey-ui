//go:build with_quic

package runtime

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type portProbeSocket struct{ closed bool }

func (s *portProbeSocket) Close() error { s.closed = true; return nil }

func TestQUICFixturePortUsesBothTransportAllocators(t *testing.T) {
	denied := errors.New("UDP range excludes TCP allocator candidate")
	var sockets []*portProbeSocket
	var calls []string
	bind := func(network, address string) (io.Closer, int, error) {
		calls = append(calls, network+" "+address)
		if network == "udp" && address == "127.0.0.1:50001" {
			return nil, 0, denied
		}
		socket := &portProbeSocket{}
		sockets = append(sockets, socket)
		if network == "tcp" && address == "127.0.0.1:0" {
			return socket, 50001, nil
		}
		return socket, 50002, nil
	}
	port, err := findQUICFixturePort(bind)
	if err != nil || port != 50002 {
		t.Fatalf("available dual-protocol port was missed: %d %v", port, err)
	}
	if strings.Join(calls, ",") != "tcp 127.0.0.1:0,udp 127.0.0.1:50001,udp 127.0.0.1:0,tcp 127.0.0.1:50002" {
		t.Fatalf("both allocators were not independently probed: %v", calls)
	}
	for _, socket := range sockets {
		if !socket.closed {
			t.Fatal("port probe leaked a socket")
		}
	}
}

func TestQUICFixturePortRequiresBothProtocolsAndReportsBindFailure(t *testing.T) {
	denied := errors.New("TCP bind denied")
	allocations := 0
	var sockets []*portProbeSocket
	bind := func(network, address string) (io.Closer, int, error) {
		if address == "127.0.0.1:0" {
			allocations++
		}
		if network == "tcp" {
			return nil, 0, denied
		}
		socket := &portProbeSocket{}
		sockets = append(sockets, socket)
		return socket, 50002, nil
	}
	port, err := findQUICFixturePort(bind)
	if port != 0 || !errors.Is(err, denied) || allocations != 32 {
		t.Fatalf("one protocol gained dual-protocol admission or lost bounds: %d %v %d", port, err, allocations)
	}
	if !strings.Contains(err.Error(), "tcp") {
		t.Fatal("bind diagnostic lost its protocol")
	}
	for _, socket := range sockets {
		if !socket.closed {
			t.Fatal("failed probe leaked a socket")
		}
	}
}

func TestQUICReplacementPortFailurePreservesFixture(t *testing.T) {
	denied := errors.New("all loopback ports denied")
	f := &quicFixture{port: 50000, inbound: map[string]any{"listen_port": 50000, "users": "accepted-users"}}
	probes := 0
	err := f.replaceWithPortProbe(func(string, string) (io.Closer, int, error) {
		probes++
		return nil, 0, denied
	}, 9)
	if !errors.Is(err, denied) || probes != 32 {
		t.Fatalf("replacement lost its bounded bind failure: probes=%d err=%v", probes, err)
	}
	if f.port != 50000 || f.inbound["listen_port"] != 50000 || f.inbound["users"] != "accepted-users" {
		t.Fatal("failed replacement changed the accepted fixture or selected an unqualified zero port")
	}
}
