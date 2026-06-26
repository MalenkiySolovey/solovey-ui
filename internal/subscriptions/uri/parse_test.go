package uri

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
)

func TestGetOutboundParsesCommonExternalLinks(t *testing.T) {
	tests := []struct {
		name   string
		link   string
		tag    string
		expect map[string]interface{}
	}{
		{
			name: "vless reality grpc",
			link: "vless://11111111-1111-4111-8111-111111111111@example.com:443?security=reality&pbk=pubkey&sid=abcd&type=grpc&serviceName=svc&sni=sni.example.com&fp=chrome&flow=xtls-rprx-vision#vless-node",
			tag:  "vless-node",
			expect: map[string]interface{}{
				"type":                   "vless",
				"server":                 "example.com",
				"server_port":            443,
				"uuid":                   "11111111-1111-4111-8111-111111111111",
				"flow":                   "xtls-rprx-vision",
				"tls.enabled":            true,
				"tls.server_name":        "sni.example.com",
				"tls.reality.enabled":    true,
				"tls.reality.public_key": "pubkey",
				"tls.reality.short_id":   "abcd",
				"tls.utls.enabled":       true,
				"tls.utls.fingerprint":   "chrome",
				"transport.type":         "grpc",
				"transport.service_name": "svc",
			},
		},
		{
			name: "trojan ws tls",
			link: "trojan://secret@example.org:8443?security=tls&sni=cdn.example.org&type=ws&host=front.example.org&path=%2Fws#trojan-node",
			tag:  "trojan-node",
			expect: map[string]interface{}{
				"type":                   "trojan",
				"server":                 "example.org",
				"server_port":            8443,
				"password":               "secret",
				"tls.enabled":            true,
				"tls.server_name":        "cdn.example.org",
				"transport.type":         "ws",
				"transport.path":         "/ws",
				"transport.headers.Host": "front.example.org",
			},
		},
		{
			name: "hysteria2 salamander mport",
			link: "hysteria2://hy-pass@hy.example.com:443?obfs=salamander&obfs-password=obfs-pass&mport=20000-20002,30000&downmbps=120&upmbps=60&fastopen=true&sni=hy.example.com#hy2-node",
			tag:  "hy2-node",
			expect: map[string]interface{}{
				"type":            "hysteria2",
				"server":          "hy.example.com",
				"server_port":     443,
				"password":        "hy-pass",
				"down_mbps":       120,
				"up_mbps":         60,
				"fastopen":        true,
				"server_ports":    []string{"20000:20002", "30000"},
				"obfs.type":       "salamander",
				"obfs.password":   "obfs-pass",
				"tls.enabled":     true,
				"tls.server_name": "hy.example.com",
			},
		},
		{
			name: "anytls",
			link: "anytls://any-pass@any.example.com:443?sni=any.example.com&allowInsecure=1#any-node",
			tag:  "any-node",
			expect: map[string]interface{}{
				"type":            "anytls",
				"server":          "any.example.com",
				"server_port":     443,
				"password":        "any-pass",
				"tls.enabled":     true,
				"tls.server_name": "any.example.com",
				"tls.insecure":    true,
			},
		},
		{
			name: "shadowsocks plugin",
			link: "ss://aes-128-gcm:ss-pass@ss.example.com:8388?plugin=v2ray-plugin%3Btls%3Bhost%3Dcdn.example.com#ss-node",
			tag:  "ss-node",
			expect: map[string]interface{}{
				"type":        "shadowsocks",
				"server":      "ss.example.com",
				"server_port": 8388,
				"method":      "aes-128-gcm",
				"password":    "ss-pass",
				"plugin":      "v2ray-plugin",
				"plugin_opts": "tls;host=cdn.example.com",
			},
		},
		{
			name: "naive quic",
			link: "naive+quic://user:pass@naive.example.com:443?peer=front.example.com&alpn=h3&insecure=true#naive-node",
			tag:  "naive-node",
			expect: map[string]interface{}{
				"type":            "naive",
				"server":          "naive.example.com",
				"server_port":     443,
				"username":        "user",
				"password":        "pass",
				"quic":            true,
				"tls.enabled":     true,
				"tls.server_name": "front.example.com",
				"tls.insecure":    true,
				"tls.alpn":        []string{"h3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbound, tag, err := Parse(tt.link, 0)
			if err != nil {
				t.Fatal(err)
			}
			if tag != tt.tag {
				t.Fatalf("tag = %q, want %q", tag, tt.tag)
			}
			if got, _ := (*outbound)["tag"].(string); got != tt.tag {
				t.Fatalf("outbound tag = %q, want %q", got, tt.tag)
			}
			for path, want := range tt.expect {
				got := nestedOutboundValue(*outbound, path)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s = %#v, want %#v\noutbound=%s", path, got, want, mustMarshalOutboundForTest(t, *outbound))
				}
			}
		})
	}
}

func TestGetOutboundVmessBase64(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"v":    "2",
		"ps":   "vmess-node",
		"add":  "vmess.example.com",
		"port": "443",
		"id":   "22222222-2222-4222-8222-222222222222",
		"aid":  0,
		"net":  "ws",
		"type": "none",
		"host": "front.example.com",
		"path": "/ws",
		"tls":  "tls",
		"sni":  "vmess.example.com",
		"alpn": "h2,http/1.1",
		"fp":   "chrome",
	})
	if err != nil {
		t.Fatal(err)
	}
	outbound, tag, err := Parse("vmess://"+uricodec.Encode(raw), 0)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "vmess-node" {
		t.Fatalf("tag = %q, want vmess-node", tag)
	}
	expect := map[string]interface{}{
		"type":                   "vmess",
		"server":                 "vmess.example.com",
		"server_port":            "443",
		"uuid":                   "22222222-2222-4222-8222-222222222222",
		"tls.enabled":            true,
		"tls.server_name":        "vmess.example.com",
		"tls.alpn":               []string{"h2", "http/1.1"},
		"tls.utls.fingerprint":   "chrome",
		"transport.type":         "ws",
		"transport.path":         "/ws",
		"transport.headers.Host": "front.example.com",
	}
	for path, want := range expect {
		got := nestedOutboundValue(*outbound, path)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v, want %#v\noutbound=%s", path, got, want, mustMarshalOutboundForTest(t, *outbound))
		}
	}
}

func TestGetOutboundRejectsUnsupportedLink(t *testing.T) {
	if _, _, err := Parse("ftp://example.com", 0); err == nil {
		t.Fatal("unsupported link returned nil error")
	}
}

func nestedOutboundValue(outbound map[string]interface{}, path string) interface{} {
	current := interface{}(outbound)
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = obj[part]
	}
	return current
}

func mustMarshalOutboundForTest(t *testing.T, outbound map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(outbound)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestGetOutboundShadowsocksBase64UserInfo(t *testing.T) {
	userInfo := url.QueryEscape(uricodec.Encode([]byte("chacha20-ietf-poly1305:secret:with:colons")))
	link := "ss://" + userInfo + "@base64-ss.example.com:8388#ss-base64"
	outbound, tag, err := Parse(link, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "ss-base64" {
		t.Fatalf("tag = %q, want ss-base64", tag)
	}
	if got := (*outbound)["method"]; got != "chacha20-ietf-poly1305" {
		t.Fatalf("method = %#v", got)
	}
	if got := (*outbound)["password"]; got != "secret:with:colons" {
		t.Fatalf("password = %#v", got)
	}
}
