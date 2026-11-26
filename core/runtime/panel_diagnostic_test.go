package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestPanelDiagnosticUsesActiveIngressDNSAndRouting(t *testing.T) {
	const host = "fixture.invalid"
	var reached atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.Host != host || r.URL.Path != "/proof" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reached.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(origin.Close)
	target := origin.Listener.Addr().(*net.TCPAddr).AddrPort()
	portTrap := panelDiagnosticTrap(t, net.IPv4(127, 0, 0, 1), 0)
	addressTrap := panelDiagnosticTrap(t, net.IPv4(127, 0, 0, 2), target.Port())
	core := NewCore()
	config := fmt.Sprintf(`{
	  "log":{"disabled":true},
	  "inbounds":[
	    {"type":"mixed","tag":"accepted","listen":"127.0.0.1"},
	    {"type":"http","tag":"address-rewrite","listen":"127.0.0.1"},
	    {"type":"mixed","tag":"port-rewrite","listen":"127.0.0.1"},
	    {"type":"mixed","tag":"unresolved","listen":"127.0.0.1"},
	    {"type":"mixed","tag":"authenticated","listen":"127.0.0.1","users":[{"username":"fixture","password":"test-only"}]}
	  ],
	  "dns":{"servers":[
	    {"type":"hosts","tag":"ingress-answer","predefined":{"fixture.invalid":"127.0.0.1"}},
	    {"type":"hosts","tag":"other-answer","predefined":{"fixture.invalid":"127.0.0.2"}}
	  ],"rules":[{"inbound":"accepted","action":"route","server":"ingress-answer"}],"final":"other-answer"},
	  "outbounds":[{"type":"block","tag":"blocked-default"},{"type":"direct","tag":"admitted-egress"}],
	  "route":{"rules":[
	    {"inbound":"accepted","action":"sniff","sniffer":"http"},
	    {"inbound":"accepted","action":"resolve","strategy":"ipv4_only"},
	    {"inbound":"accepted","protocol":"http","domain":"fixture.invalid","action":"route","outbound":"admitted-egress"},
	    {"inbound":"address-rewrite","action":"route","outbound":"admitted-egress","override_address":"127.0.0.2"},
	    {"inbound":"port-rewrite","action":"route","outbound":"admitted-egress","override_port":%d},
	    {"inbound":"unresolved","action":"route","outbound":"admitted-egress"}
	  ],"final":"blocked-default"}
	}`, portTrap.Addr().(*net.TCPAddr).Port)
	if err := core.Start([]byte(config)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	if choices := core.PanelDiagnosticChoices(); !slices.Equal(choices, []string{"accepted", "address-rewrite", "port-rewrite", "unresolved"}) {
		t.Fatalf("unexpected active choices: %v", choices)
	}
	if _, err := core.OpenPanelDiagnostic("authenticated"); !errors.Is(err, ErrDiagnosticIngress) {
		t.Fatalf("authenticated ingress was offered as anonymous: %v", err)
	}
	permit := func(addr netip.Addr) bool { return addr == target.Addr() }
	for _, tc := range []struct {
		ingress string
		want    error
	}{
		{"accepted", nil}, {"address-rewrite", ErrDiagnosticTarget}, {"port-rewrite", ErrDiagnosticTarget}, {"unresolved", ErrDiagnosticPanelDNS},
	} {
		t.Run(tc.ingress, func(t *testing.T) {
			path, err := core.OpenPanelDiagnostic(tc.ingress)
			if err != nil || path.Generation() == "" {
				t.Fatalf("open: %v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if tc.want == nil {
				answer, err := path.Resolve(ctx, host, "dns")
				if err != nil || !slices.Equal(answer.Addresses, []netip.Addr{target.Addr()}) {
					t.Fatalf("lookup did not select the active ingress DNS rule: %+v, %v", answer, err)
				}
				if _, err := path.Resolve(ctx, host, "doh"); !errors.Is(err, ErrDiagnosticResolver) {
					t.Fatalf("lookup claimed a transport it does not observe: %v", err)
				}
			}
			if conn, _, err := path.DialStream(ctx, host, netip.AddrPortFrom(target.Addr(), 443), permit); conn != nil || !errors.Is(err, ErrDiagnosticTarget) {
				t.Fatalf("the public API admitted a private target: %v", err)
			}
			// The loopback-only fixture exercises dispatch; it does not change
			// the public target/port policy above.
			conn, observe, err := path.dialStream(ctx, host, target, permit)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if tc.want == nil && observe().Egress != "" {
				t.Fatal("the CONNECT handshake alone was promoted to route evidence before sniffing")
			}
			_, writeErr := io.WriteString(conn, "HEAD /proof HTTP/1.1\r\nHost: fixture.invalid\r\nConnection: close\r\n\r\n")
			response, readErr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodHead})
			observation := observe()
			if tc.want == nil {
				if writeErr != nil || readErr != nil || response.StatusCode != http.StatusNoContent || observation.Egress != "admitted-egress" || observation.Err != nil {
					t.Fatalf("response did not traverse the selected ingress and outbound: %v, %v, %v, %+v", writeErr, readErr, response, observation)
				}
			} else if readErr == nil || observation.Egress != "" || !errors.Is(observation.Err, tc.want) {
				t.Fatalf("wrong route disposition: %v, %+v", readErr, observation)
			}
		})
	}
	if reached.Load() != 1 {
		t.Fatalf("origin received %d probes, want 1", reached.Load())
	}
	for _, trap := range []*net.TCPListener{addressTrap, portTrap} {
		_ = trap.SetDeadline(time.Now().Add(100 * time.Millisecond))
		if conn, err := trap.AcceptTCP(); err == nil {
			_ = conn.Close()
			t.Fatalf("an unadmitted destination received a SYN: %s", trap.Addr())
		} else if failure, ok := err.(net.Error); !ok || !failure.Timeout() {
			t.Fatalf("rewrite trap was not observable: %v", err)
		}
	}
	path, err := core.OpenPanelDiagnostic("accepted")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, observe, err := path.dialStream(ctx, host, target, permit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("HEAD / HTTP/1.0\r\n\r\n")); err == nil || !errors.Is(observe().Err, ErrStaleGeneration) {
		t.Fatalf("retired stream remains connected: %v, %+v", err, observe())
	}
	if _, err := path.Resolve(ctx, host, "dns"); !errors.Is(err, ErrCoreUnavailable) {
		t.Fatalf("retired diagnostic resolved via another generation: %v", err)
	}
}

func panelDiagnosticTrap(t *testing.T, address net.IP, port uint16) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: address, Port: int(port)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}
