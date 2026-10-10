package runtime

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"
)

// A service owner's expected HTTP/listener closure must not force the config
// save coordinator to recover by replacing the complete runtime generation.
func TestSSMHotReplacementKeepsRuntimeGenerationAndPortReusable(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	service := map[string]any{"type": "ssm-api", "tag": "ssm", "listen": "127.0.0.1", "listen_port": port, "servers": map[string]string{"/main": "managed"}}
	raw, _ := json.Marshal(map[string]any{
		"log":      map[string]bool{"disabled": true},
		"inbounds": []any{map[string]any{"type": "shadowsocks", "tag": "managed", "listen": "127.0.0.1", "listen_port": 0, "method": "aes-128-gcm", "managed": true}},
		"services": []any{service},
	})
	core := NewCore()
	if err = core.Start(raw); err != nil {
		t.Fatal("start managed SSM runtime", err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	generation, privateAPI, instance, tracker := core.managerGeneration, core.privateAPI, core.instance, core.statsTracker
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	ready := func() {
		t.Helper()
		response, err := client.Get("http://" + address + "/main/server/v1/stats")
		if err != nil {
			t.Fatal("SSM HTTP readiness", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal("SSM stats unavailable")
		}
	}
	ready()
	config, _ := json.Marshal(service)
	for range 3 {
		if err = core.RemoveService("ssm"); err != nil {
			t.Fatal("normal service removal rejected", err)
		}
		probe, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal("removed service listener leaked", err)
		}
		probe.Close()
		if err = core.AddService(config); err != nil {
			t.Fatal("normal service replacement rejected", err)
		}
		ready()
		if core.managerGeneration != generation || core.privateAPI != privateAPI || core.instance != instance || core.statsTracker != tracker {
			t.Fatal("service replacement disturbed the owning runtime")
		}
	}
}
