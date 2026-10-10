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
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"github.com/sagernet/sing-box/option"
)

func ssmBox(t *testing.T, cache string, port int) *box.Box {
	t.Helper()
	ctx := registry.Context(context.Background())
	raw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "inbounds": []any{map[string]any{"type": "shadowsocks", "tag": "managed", "listen": "127.0.0.1", "listen_port": 0, "method": "aes-128-gcm", "managed": true}}, "services": []any{map[string]any{"type": "ssm-api", "tag": "ssm", "listen": "127.0.0.1", "listen_port": port, "servers": map[string]string{"/main": "managed"}, "cache_path": cache}}})
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
		t.Fatal("schema", err)
	}
	b, err := box.NewBox(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal("constructor", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func ssmPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
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
			port := ssmPort(t)
			b := ssmBox(t, name, port)
			if err = b.Start(); err == nil {
				t.Fatal("corrupt service started")
			}
			_ = b.Close()
			got, _ := os.ReadFile(name)
			if string(got) != data {
				t.Fatal("failed startup overwrote recoverable cache")
			}
			if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond); err == nil {
				connection.Close()
				t.Fatal("failed startup leaked listener")
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
		port := ssmPort(t)
		b := ssmBox(t, name, port)
		if err = b.Start(); err != nil {
			t.Fatal("service start", err)
		}
		base := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/main/server/v1"
		response, err := client.Get(base + "/users/fixture")
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
		response, err = client.Get(base + "/stats")
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
		if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond); err == nil {
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
