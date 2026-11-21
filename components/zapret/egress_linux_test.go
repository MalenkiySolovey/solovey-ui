//go:build linux && zapretproof && !minimal

package zapret

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	M "github.com/sagernet/sing/common/metadata"
)

// Run after the loopback adapter has retired, in the same disposable namespace.
// Dummy egress devices discard packets; nft counts which interface routing chose.
func qualifyZapretEgressPin(t *testing.T, ctx context.Context, mark uint32) {
	t.Helper()
	const peer = "203.0.113.53"
	const pinnedDev = "r8a-pin-a"
	const alternateDev = "r8a-pin-b"
	run := func(tool, input string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, tool, args...)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", tool, args, err, out)
		}
		return out
	}
	for _, egress := range []struct{ device, source, metric string }{
		{pinnedDev, "192.0.2.1", "100"}, {alternateDev, "192.0.2.2", "200"},
	} {
		run("ip", "", "link", "add", egress.device, "type", "dummy")
		run("ip", "", "link", "set", egress.device, "up")
		run("ip", "", "-4", "addr", "add", egress.source+"/32", "dev", egress.device)
		run("ip", "", "-4", "route", "add", peer+"/32", "dev", egress.device, "src", egress.source, "metric", egress.metric)
	}
	run("nft", fmt.Sprintf(`table inet fixture_egress_pin {
 counter on_pinned {}
 counter on_alternate {}
 chain observed {
  type filter hook output priority 150; policy accept;
  ip daddr %s udp dport 12345 meta mark 0x%x oifname %q counter name on_pinned
  ip daddr %s udp dport 12345 meta mark 0x%x oifname %q counter name on_alternate
 }
}
`, peer, mark, pinnedDev, peer, mark, alternateDev), "-f", "-")
	counter := func(name string) uint64 {
		t.Helper()
		var listing struct {
			NFTables []struct {
				Counter *struct{ Packets uint64 }
			}
		}
		raw := run("nft", "", "-j", "list", "counter", "inet", "fixture_egress_pin", name)
		if err := json.Unmarshal(raw, &listing); err != nil || len(listing.NFTables) != 2 || listing.NFTables[1].Counter == nil {
			t.Fatalf("unexpected egress counter: %s (%v)", raw, err)
		}
		return listing.NFTables[1].Counter.Packets
	}
	core := coreruntime.NewCore()
	config := fmt.Sprintf(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"pinned","bind_interface":%q,"routing_mark":%d},{"type":"direct","tag":"mark-only","routing_mark":%d}],"route":{"final":"mark-only"}}`, pinnedDev, mark, mark)
	if err := core.Start([]byte(config)); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	dial := func(tag string) (net.Conn, error) {
		return core.DialOutbound(ctx, tag, "udp", M.ParseSocksaddr(peer+":12345"))
	}
	probe, err := dial("pinned")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if _, err := probe.Write([]byte{1}); err != nil || counter("on_pinned") != 1 || counter("on_alternate") != 0 {
		t.Fatalf("initial pinned send did not use its chosen interface: %v", err)
	}
	run("ip", "", "-4", "route", "del", peer+"/32", "dev", pinnedDev, "metric", "100")
	_, reuseErr := probe.Write([]byte{2})
	if fresh, dialErr := dial("pinned"); dialErr == nil {
		_, _ = fresh.Write([]byte{3})
		_ = fresh.Close()
	}
	if got := counter("on_alternate"); got != 0 {
		t.Fatalf("pinned Direct leaked %d packets to the alternate interface", got)
	}
	control, err := dial("mark-only")
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if _, err := control.Write([]byte{4}); err != nil || counter("on_alternate") != 1 {
		t.Fatalf("mark-only control did not take the alternate route: %v", err)
	}
	t.Logf("EGRESS_PIN_PROVEN: existing/fresh pinned IPv4 UDP emitted 0 on alternate, mark-only control emitted 1; existing write: %v", reuseErr)
}
