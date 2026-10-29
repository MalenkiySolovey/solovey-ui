package registry_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

func ssmBox(t *testing.T, cache string, port int) *box.Box {
	t.Helper()
	ctx := registry.Context(context.Background())
	raw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": false}, "inbounds": []any{map[string]any{"type": "shadowsocks", "tag": "managed", "listen": "127.0.0.1", "listen_port": 0, "method": "aes-128-gcm", "managed": true}}, "services": []any{map[string]any{"type": "ssm-api", "tag": "ssm", "listen": "127.0.0.1", "listen_port": port, "servers": map[string]string{"/main": "managed"}, "cache_path": cache}}})
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
		t.Fatal("schema", err)
	}
	options.Log.DisableColor = true
	b, err := box.NewBox(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal("constructor", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// Observe the service's actual kernel-assigned port. Closing a reservation
// before Box.Start lets parallel tests or outgoing sockets steal that port.
type ssmListenerObservation struct {
	mu      sync.Mutex
	address string
	prefix  string
}

func (*ssmListenerObservation) DisableColors() bool { return true }

func (observation *ssmListenerObservation) WriteMessage(_ log.Level, message string) {
	prefix := observation.prefix
	if prefix == "" {
		prefix = "service/ssm-api[ssm]: tcp server started at "
	}
	if index := strings.Index(message, prefix); index >= 0 {
		observation.mu.Lock()
		observation.address = strings.TrimSpace(message[index+len(prefix):])
		observation.mu.Unlock()
	}
}

func (observation *ssmListenerObservation) Address() string {
	observation.mu.Lock()
	defer observation.mu.Unlock()
	return observation.address
}

func TestRealmServiceNativeAuthenticationAndClosedGeneration(t *testing.T) {
	if !registry.Resolve("services", "hysteria-realm").Available() {
		t.Skip("Realm is absent from this provider/profile")
	}
	ctx := registry.Context(context.Background())
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, []byte(`{"log":{"disabled":false},"services":[{"type":"hysteria-realm","tag":"realm","listen":"127.0.0.1","listen_port":0,"users":[{"name":"operator","token":"realm-runtime-canary","max_realms":1}]}]}`)); err != nil {
		t.Fatal(err)
	}
	options.Log.DisableColor = true
	b, err := box.NewBox(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	observed := &ssmListenerObservation{prefix: "service/hysteria-realm[realm]: tcp server started at "}
	b.LogFactory().AttachPlatformWriter(observed)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	address := observed.Address()
	if address == "" {
		t.Fatal("realm did not publish its listener")
	}
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	request := func(realm, token string) (int, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/"+realm+"/", strings.NewReader(`{"addresses":["127.0.0.1:12345"]}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		if response.ProtoMajor != 2 {
			t.Fatal("realm did not serve HTTP/2")
		}
		return response.StatusCode, nil
	}
	if status, err := request("denied", "wrong"); err != nil || status != http.StatusUnauthorized {
		t.Fatal("realm admitted an invalid token", status, err)
	}
	if status, err := request("first", "realm-runtime-canary"); err != nil || status != http.StatusOK {
		t.Fatal("realm registration failed", status, err)
	}
	if status, err := request("second", "realm-runtime-canary"); err != nil || status != http.StatusTooManyRequests {
		t.Fatal("realm lost its identity quota", status, err)
	}
	closeErr := b.Close()
	if _, err := request("retired", "realm-runtime-canary"); err == nil {
		t.Fatal("closed realm generation still accepts authenticated HTTP/2 requests")
	}
	if closeErr != nil {
		t.Fatal("realm close failed", closeErr)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("realm did not release its port", err)
	}
	_ = listener.Close()
}

func TestSSMBoxCorruptCacheFailClosedWithoutPanicOrPublication(t *testing.T) {
	for _, data := range []string{"null", `{"endpoints":{"/main":null}}`, "{"} {
		t.Run(data, func(t *testing.T) {
			t.Setenv("SUI_DB_FOLDER", t.TempDir())
			name := filepath.Join(ssmcache.Root(), "state.json")
			store, err := ssmcache.New(name)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Write(context.Background(), []byte("{}")); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(name, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			b := ssmBox(t, name, 0)
			observed := &ssmListenerObservation{}
			b.LogFactory().AttachPlatformWriter(observed)
			if err = b.Start(); err == nil {
				t.Fatal("corrupt service started")
			}
			_ = b.Close()
			got, _ := os.ReadFile(name)
			if string(got) != data {
				t.Fatal("failed startup overwrote recoverable cache")
			}
			if observed.Address() != "" {
				t.Fatal("corrupt cache published a service listener before rejection")
			}
		})
	}
}

func TestSSMBoxRestartRestoresOfficialAuthenticationAndCounters(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	name := filepath.Join(ssmcache.Root(), "state.json")
	credential := rand.Text()
	data, _ := json.Marshal(map[string]any{"endpoints": map[string]any{"/main": map[string]any{"global_uplink": 17, "user_uplink": map[string]int{"fixture": 9}, "users": map[string]string{"fixture": credential}}}})
	store, err := ssmcache.New(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Write(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	client := http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for restart := 0; restart < 2; restart++ {
		b := ssmBox(t, name, 0)
		observed := &ssmListenerObservation{}
		b.LogFactory().AttachPlatformWriter(observed)
		if err = b.Start(); err != nil {
			t.Fatal("service start", err)
		}
		address := observed.Address()
		host, port, err := net.SplitHostPort(address)
		parsedPort, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || host != "127.0.0.1" || parsedPort <= 0 {
			t.Fatal("service did not publish its actual loopback listener")
		}
		base := "http://" + address + "/main/server/v1"
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/users/fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var user struct {
			Password string `json:"uPSK"`
			Uplink   int    `json:"uplinkBytes"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&user)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || user.Password != credential || user.Uplink != 9 {
			t.Fatal("official authenticated service state lost")
		}
		request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/stats", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var stats struct {
			Uplink int `json:"uplinkBytes"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&stats)
		response.Body.Close()
		if err != nil || stats.Uplink != 17 {
			t.Fatal("official service counters lost")
		}
		if err = b.Close(); err != nil {
			t.Fatal("shutdown", err)
		}
		if connection, err := net.DialTimeout("tcp", address, 300*time.Millisecond); err == nil {
			connection.Close()
			t.Fatal("closed service leaked listener")
		}
	}
}

func TestSSMBoxPartialStartFailurePreservesCacheAndAbsentCacheIsOptional(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	name := filepath.Join(ssmcache.Root(), "state.json")
	store, err := ssmcache.New(name)
	if err != nil {
		t.Fatal(err)
	}
	preimage := []byte("{}")
	if err = store.Write(context.Background(), preimage); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	b := ssmBox(t, name, l.Addr().(*net.TCPAddr).Port)
	if err = b.Start(); err == nil {
		t.Fatal("occupied service listener accepted")
	}
	_ = b.Close()
	got, _ := os.ReadFile(name)
	if !bytes.Equal(got, preimage) {
		t.Fatal("partial startup saved cache")
	}
	b = ssmBox(t, "", 0)
	if err = b.Start(); err != nil {
		t.Fatal("absent cache changed service startup", err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(ssmcache.Root())
	if len(entries) != 1 {
		t.Fatal("absent optional cache persisted state")
	}
}
