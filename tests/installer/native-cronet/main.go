//go:build linux && with_musl && !with_purego

// Native ABI regression for the same pinned Cronet archive as the Docker panel.
// Run in the final non-root Alpine image with no network or extra capabilities.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/cronet-go"
	_ "github.com/sagernet/cronet-go/all"
	M "github.com/sagernet/sing/common/metadata"
)

func main() {
	if os.Geteuid() != 65532 {
		fail("NON_ROOT_IDENTITY_REQUIRED")
	}
	// Query the real native library, not a version constant from a Go wrapper.
	engine := cronet.NewEngine()
	version := engine.Version()
	engine.Destroy()
	if !strings.Contains(version, "150.0.7871.63") {
		fail("NATIVE_VERSION_MISMATCH")
	}
	started := time.Now()
	for round := 0; round < 3; round++ {
		for _, quic := range []bool{false, true} {
			client, err := cronet.NewNaiveClient(cronet.NaiveClientOptions{
				Context: context.Background(), ServerAddress: M.ParseSocksaddr("127.0.0.1:443"),
				ServerName: "native-abi.invalid", QUIC: quic,
				DNSResolver: func(_ context.Context, request *dns.Msg) *dns.Msg {
					return new(dns.Msg).SetRcode(request, dns.RcodeNameError)
				},
			})
			if err != nil {
				fail("NATIVE_CLIENT_CONSTRUCTION_FAILED")
			}
			if err := client.Start(); err != nil {
				fail("NATIVE_CLIENT_START_FAILED")
			}
			// Start creates the actual native engine and socket-pair workers.
			// Close must join workers and shut down/destroy the engine. The outer
			// container timeout bounds a stuck native call without correctness sleeps.
			if err := client.Close(); err != nil {
				fail("NATIVE_CLIENT_CLOSE_FAILED")
			}
			if err := client.Close(); err != net.ErrClosed {
				fail("NATIVE_CLIENT_CLOSED_STATE_MISMATCH")
			}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"result": "PASS", "uid": os.Geteuid(), "architecture": runtime.GOARCH,
		"nativeVersion": version, "completedLifecycles": 6, "elapsedMs": time.Since(started).Milliseconds(),
		"trafficQualification": "NOT_CLAIMED", "externalNetwork": "NONE",
	})
}

func fail(code string) {
	fmt.Fprintln(os.Stderr, code)
	os.Exit(1)
}
