package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sagernet/sing-box/daemon"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const privateFixture = `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"direct","tag":"second"},{"type":"selector","tag":"choice","outbounds":["direct","second"]}]}`

func startPrivateFixture(t *testing.T) *Core {
	t.Helper()
	c := NewCore()
	if err := c.Start([]byte(privateFixture)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c
}

func TestPrivateAPIAuthAndGeneratedPolicy(t *testing.T) {
	c := startPrivateFixture(t)
	api := c.privateAPI
	if len(api.secret) < 64 || api.reservation != nil || api.conn == nil {
		t.Fatal("unaccepted API credential/resource")
	}
	host, _, err := net.SplitHostPort(api.endpoint)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("non-loopback API")
	}
	if fact := c.RuntimeStatus(context.Background()); !fact.APIAvailable || fact.State != "running" || fact.Generation != api.generation {
		t.Fatalf("status=%+v", fact)
	}
	services := c.serviceManager.Services()
	if len(services) != 1 || services[0].Tag() != privateAPITag || services[0].Type() != "api" {
		t.Fatal("missing single generated official API service")
	}
	conn, err := grpc.NewClient("passthrough:///"+api.endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := daemon.NewStartedServiceClient(conn)
	for _, auth := range []string{"", "Bearer incorrect", "Basic incorrect", "Bearer "} {
		t.Run("rejected-"+strings.Split(auth, " ")[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if auth != "" {
				ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", auth))
			}
			_, err := client.GetVersion(ctx, &emptypb.Empty{})
			assertUnauthenticated(t, err)
			_, err = grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
			assertUnauthenticated(t, err)
			stream, err := client.SubscribeServiceStatus(ctx, &emptypb.Empty{})
			if err == nil {
				_, err = stream.Recv()
			}
			assertUnauthenticated(t, err)
			reflection, err := grpc_reflection_v1.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
			if err == nil {
				err = reflection.Send(&grpc_reflection_v1.ServerReflectionRequest{MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{ListServices: ""}})
			}
			// A server-side rejection can make Send return EOF before the RPC status.
			if err == nil || errors.Is(err, io.EOF) {
				_, err = reflection.Recv()
			}
			assertUnauthenticated(t, err)
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+api.secret))
	if _, err := client.GetVersion(ctx, &emptypb.Empty{}); err != nil {
		t.Fatal("valid bearer rejected")
	}
	if _, err := daemon.NewManagedServiceClient(conn).StopService(ctx, &emptypb.Empty{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("attached API exposed host lifecycle")
	}
	encoded, err := json.Marshal(c.RuntimeStatus(context.Background()))
	if err != nil || bytes.Contains(encoded, []byte(api.secret)) || bytes.Contains(encoded, []byte(api.endpoint)) {
		t.Fatal("private credential or endpoint leaked into status")
	}
}

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("auth code=%v", status.Code(err))
	}
}

func TestPrivateAPIWebAccessDashboardAndCORS(t *testing.T) {
	c := startPrivateFixture(t)
	base := "http://" + c.privateAPI.endpoint
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("dashboard unexpectedly enabled")
	}
	request, _ = http.NewRequestWithContext(t.Context(), http.MethodOptions, base+"/daemon.StartedService/GetVersion", nil)
	request.Header.Set("Origin", "https://operator.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type,x-grpc-web,authorization")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.Header.Get("Access-Control-Allow-Origin") == "*" || response.Header.Get("Access-Control-Allow-Origin") == "https://operator.example" {
		t.Fatal("browser origin allowed")
	}
	for _, auth := range []string{"", "Bearer incorrect"} {
		request, _ = http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/daemon.StartedService/GetVersion", bytes.NewReader(make([]byte, 5)))
		request.Header.Set("Content-Type", "application/grpc-web+proto")
		request.Header.Set("X-Grpc-Web", "1")
		if auth != "" {
			request.Header.Set("Authorization", auth)
		}
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.Header.Get("Grpc-Status") != "16" && !bytes.Contains(body, []byte("grpc-status: 16")) {
			t.Fatalf("gRPC-Web missing auth rejection: status=%d", response.StatusCode)
		}
	}
	for _, auth := range []string{"", "Bearer incorrect"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, response, err := websocket.Dial(ctx, "ws://"+c.privateAPI.endpoint+"/daemon.StartedService/SubscribeServiceStatus", &websocket.DialOptions{Subprotocols: []string{"grpc-websockets"}})
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		headers := "content-type: application/grpc\r\n"
		if auth != "" {
			headers += "authorization: " + auth + "\r\n"
		}
		err = conn.Write(ctx, websocket.MessageBinary, []byte(headers+"\r\n"))
		if err == nil {
			err = conn.Write(ctx, websocket.MessageBinary, append([]byte{0}, make([]byte, 5)...))
		}
		if err == nil {
			err = conn.Write(ctx, websocket.MessageBinary, []byte{1})
		}
		var body []byte
		if err == nil {
			_, body, err = conn.Read(ctx)
		}
		_ = conn.CloseNow()
		cancel()
		if err != nil || !bytes.Contains(body, []byte("grpc-status: 16")) {
			t.Fatal("WebSocket stream omitted bearer rejection")
		}
	}
}

func TestPrivateAPIPartialStartupAndCollisionDoNotPublish(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "constructor", true: "collision"}[collision], func(t *testing.T) {
			c := NewCore()
			var endpoint string
			var blocker net.Listener
			c.prepareAPI = func(ctx context.Context, opts *option.Options) (context.Context, *privateAPI, error) {
				ctx, api, err := preparePrivateAPI(ctx, opts)
				if err != nil {
					return ctx, api, err
				}
				endpoint = api.endpoint
				if collision {
					_ = api.releaseReservation()
					blocker, err = net.Listen("tcp4", endpoint)
					if err != nil {
						t.Fatal(err)
					}
				}
				return ctx, api, nil
			}
			fixture := []byte(`{"inbounds":[{"type":"http","tag":"in","tls":{"enabled":true,"certificate_provider":"missing"}}]}`)
			if collision {
				fixture = []byte(privateFixture)
			}
			if c.Start(fixture) == nil {
				t.Fatal("failed candidate accepted")
			}
			if blocker != nil {
				_ = blocker.Close()
			}
			if c.IsRunning() || c.privateAPI != nil || c.managerGeneration != 0 || c.instance != nil {
				t.Fatal("partial candidate published")
			}
			assertEndpointReleased(t, endpoint)
			if c.RuntimeStatus(context.Background()).State != "stopped_by_error" {
				t.Fatal("failure hidden as operator stop")
			}
		})
	}
}

func TestPrivateAPIHandshakeFailureAndGenerationRotation(t *testing.T) {
	c := NewCore()
	var endpoint string
	c.prepareAPI = func(ctx context.Context, opts *option.Options) (context.Context, *privateAPI, error) {
		ctx, api, err := preparePrivateAPI(ctx, opts)
		if err == nil {
			endpoint = api.endpoint
			opts.Services[len(opts.Services)-1].Options.(*option.APIServiceOptions).Secret = "different-nonempty-test-credential"
		}
		return ctx, api, err
	}
	if c.Start([]byte(privateFixture)) == nil || c.privateAPI != nil || c.IsRunning() {
		t.Fatal("unauthenticated candidate accepted")
	}
	assertEndpointReleased(t, endpoint)
	c.prepareAPI = nil
	if err := c.Start([]byte(privateFixture)); err != nil {
		t.Fatal(err)
	}
	oldGeneration, oldSecret, oldEndpoint := c.privateAPI.generation, c.privateAPI.secret, c.privateAPI.endpoint
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	assertEndpointReleased(t, oldEndpoint)
	if err := c.Start([]byte(privateFixture)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	if oldGeneration == c.privateAPI.generation || oldSecret == c.privateAPI.secret {
		t.Fatal("replacement reused identity/credential")
	}
	if err := c.SelectRuntimeGroup(context.Background(), oldGeneration, "choice", "second"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("stale action accepted")
	}
	if _, err := c.Groups(context.Background(), oldGeneration); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("stale read accepted")
	}
}

func assertEndpointReleased(t *testing.T, endpoint string) {
	t.Helper()
	listener, err := net.Listen("tcp4", endpoint)
	if err != nil {
		t.Fatal("failed candidate retained listener")
	}
	_ = listener.Close()
}

func TestPrivateAPIRejectsRawServiceAndReservedTag(t *testing.T) {
	for _, fixture := range []string{`{"services":[{"type":"api","listen":"0.0.0.0","listen_port":0}]}`, `{"inbounds":[{"type":"http","tag":"__solovey_runtime_api"}]}`} {
		c := NewCore()
		if c.Start([]byte(fixture)) == nil || c.IsRunning() {
			t.Fatal("raw private API policy bypass")
		}
	}
	c := startPrivateFixture(t)
	if c.AddService([]byte(`{"type":"api","tag":"rogue","listen":"0.0.0.0","secret":"nonempty-test-only"}`)) == nil {
		t.Fatal("hot mutation enabled raw API")
	}
	if c.RemoveService(privateAPITag) == nil {
		t.Fatal("hot mutation removed lifecycle-owned API")
	}
}

func TestRuntimeGroupsSelectionAndAPIDegradation(t *testing.T) {
	c := startPrivateFixture(t)
	generation := c.privateAPI.generation
	groups, err := c.Groups(context.Background(), generation)
	if err != nil || len(groups.Groups) != 1 || len(groups.Groups[0].Items) != 2 {
		t.Fatalf("groups=%+v error=%v", groups, err)
	}
	if err := c.SelectRuntimeGroup(context.Background(), generation, "choice", "second"); err != nil {
		t.Fatal(err)
	}
	groups, err = c.Groups(context.Background(), generation)
	if err != nil || groups.Groups[0].Selected != "second" {
		t.Fatal("selection not reflected in official snapshot")
	}
	if err := c.SelectRuntimeGroup(context.Background(), generation, "choice", "missing"); err == nil {
		t.Fatal("unknown group member accepted")
	}
	for _, item := range c.serviceManager.Services() {
		if item.Tag() == privateAPITag {
			_ = item.Close()
		}
	}
	fact := c.RuntimeStatus(context.Background())
	if !c.IsRunning() || fact.State != "running" || fact.APIAvailable || fact.Reason != "runtime_api_unavailable" {
		t.Fatal("API failure became false core failure")
	}
}

func TestRuntimeLogProjectionRedactsEphemeralCredential(t *testing.T) {
	c := startPrivateFixture(t)
	secret := c.privateAPI.secret
	factory := service.FromContext[log.Factory](c.instance.Context())
	factory.Logger().Info("bare canary: ", secret, " password=private-canary")
	seen := false
	err := c.SubscribeRuntimeLogs(context.Background(), c.privateAPI.generation, func(event RuntimeLogEvent) bool {
		if strings.Contains(event.Message, secret) || strings.Contains(event.Message, "private-canary") {
			t.Fatal("runtime log secret leak")
		}
		if strings.Contains(event.Message, "bare canary:") {
			seen = true
			return false
		}
		return true
	})
	if err != nil || !seen {
		t.Fatal("bounded log projection did not observe existing log")
	}
}

func TestPrivateAPIReflectionRejectionBeforeSend(t *testing.T) {
	c := startPrivateFixture(t)
	conn, err := grpc.NewClient("passthrough:///"+c.privateAPI.endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := grpc_reflection_v1.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the real server to reject this unauthenticated stream before sending.
	if _, err := stream.Header(); err != nil {
		t.Fatal(err)
	}
	err = stream.Send(&grpc_reflection_v1.ServerReflectionRequest{MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{ListServices: ""}})
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("early rejection send code=%v; want queued send or transport EOF", status.Code(err))
	}
	_, err = stream.Recv()
	assertUnauthenticated(t, err)
}
