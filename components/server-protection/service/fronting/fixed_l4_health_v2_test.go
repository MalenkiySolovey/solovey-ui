package fronting

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestFixedL4LoopbackHealthProvesExactBackendAndProxyMode(t *testing.T) {
	for _, mode := range []hostresources.ProxyMode{hostresources.ProxyModeOff, hostresources.ProxyModeOn} {
		t.Run(string(mode), func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			backend, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			alternate, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer alternate.Close()
			public, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer public.Close()
			var backendReceipts, alternateReceipts atomic.Uint32
			backendPayload := make(chan string, 1)
			alternateDone := make(chan struct{})
			_ = alternate.(*net.TCPListener).SetDeadline(time.Now().Add(500 * time.Millisecond))
			go func() {
				defer close(alternateDone)
				connection, acceptErr := alternate.Accept()
				if acceptErr == nil {
					alternateReceipts.Add(1)
					_ = connection.Close()
				}
			}()
			go func() {
				connection, acceptErr := backend.Accept()
				if acceptErr != nil {
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(connection)
				payload := ""
				if mode == hostresources.ProxyModeOn {
					line, _ := reader.ReadString('\n')
					payload += line
				}
				request := make([]byte, len("health-nonce"))
				_, _ = io.ReadFull(reader, request)
				payload += string(request)
				backendReceipts.Add(1)
				backendPayload <- payload
				_, _ = io.WriteString(connection, "backend:"+strings.Repeat("b", 64))
			}()
			go func() {
				connection, acceptErr := public.Accept()
				if acceptErr != nil {
					return
				}
				defer connection.Close()
				upstream, dialErr := net.DialTimeout("tcp4", backend.Addr().String(), 2*time.Second)
				if dialErr != nil {
					return
				}
				defer upstream.Close()
				if mode == hostresources.ProxyModeOn {
					_, _ = fmt.Fprintf(upstream, "PROXY TCP4 127.0.0.1 127.0.0.1 12345 %d\r\n", backend.Addr().(*net.TCPAddr).Port)
				}
				request := make([]byte, len("health-nonce"))
				_, _ = io.ReadFull(connection, request)
				_, _ = upstream.Write(request)
				response := make([]byte, len("backend:")+64)
				_, _ = io.ReadFull(upstream, response)
				_, _ = connection.Write(response)
			}()

			request := FixedL4HealthRequestV2{OperationID: "operation-health", OperationRevision: 7, PlanDigest: strings.Repeat("a", 64),
				CandidateRevision: strings.Repeat("c", 64), CandidateSHA256: strings.Repeat("d", 64), SocketClaimRevision: strings.Repeat("e", 64),
				BackendReferenceRevision: strings.Repeat("b", 64), LeaseRevision: strings.Repeat("f", 64), ProxyMode: mode}
			started := time.Now()
			connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.Background(), "tcp4", public.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.WriteString(connection, "health-nonce")
			response := make([]byte, len("backend:")+64)
			_, err = io.ReadFull(connection, response)
			_ = connection.Close()
			if err != nil || string(response) != "backend:"+strings.Repeat("b", 64) {
				t.Fatalf("response=%q err=%v", response, err)
			}
			latency := time.Since(started)
			payload := <-backendPayload
			<-alternateDone
			proxyObserved := strings.HasPrefix(payload, "PROXY TCP4 ")
			evidence := FixedL4HealthEvidenceV2{Schema: FixedL4HealthSchemaV2, OperationID: request.OperationID, OperationRevision: request.OperationRevision,
				PlanDigest: request.PlanDigest, CandidateRevision: request.CandidateRevision, CandidateSHA256: request.CandidateSHA256,
				SocketClaimRevision: request.SocketClaimRevision, BackendReferenceRevision: request.BackendReferenceRevision, LeaseRevision: request.LeaseRevision,
				ProxyMode: mode, PublicFixtureAccepted: true, ExpectedBackendReached: backendReceipts.Load() == 1,
				BackendIdentityMarker: request.BackendReferenceRevision, AlternateTargetReceipts: alternateReceipts.Load(), ProxyHeaderObserved: proxyObserved,
				LatencyMilliseconds: uint32(latency.Milliseconds()), ObservedAt: now.Unix(), ExpiresAt: now.Add(20 * time.Second).Unix()}
			if err := validateHealthEvidenceV2(request, evidence, now); err != nil {
				t.Fatalf("evidence=%#v payload=%q err=%v", evidence, payload, err)
			}
			t.Logf("fixed L4 loopback health: proxy=%s latency_ms=%d expected_receipts=%d alternate_receipts=%d", mode,
				evidence.LatencyMilliseconds, backendReceipts.Load(), alternateReceipts.Load())
			evidence.ProxyHeaderObserved = !proxyObserved
			if validateHealthEvidenceV2(request, evidence, now) == nil {
				t.Fatal("PROXY mode mismatch was accepted")
			}
		})
	}
}
