package uri

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func wave4JSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func wave4Inbound(t *testing.T, protocol, host string, options, outbound map[string]any) *model.Inbound {
	t.Helper()
	if options == nil {
		options = map[string]any{}
	}
	return &model.Inbound{Type: protocol, Tag: "node /?#% 雪", Options: wave4JSON(t, options), OutJson: wave4JSON(t, outbound), Addrs: wave4JSON(t, []map[string]any{{"server": host, "server_port": 443, "remark": "-public"}})}
}

func TestWave4ReservedCredentialsAndIPv6RoundTrip(t *testing.T) {
	username, password := "user@/ ?#%雪", "p@:/?# %雪"
	for _, protocol := range []string{"socks", "http", "mixed", "naive", "trojan", "anytls", "hysteria", "hysteria2", "tuic", "shadowsocks"} {
		t.Run(protocol, func(t *testing.T) {
			credential := map[string]any{"username": username, "password": password, "auth_str": password, "uuid": "11111111-1111-4111-8111-111111111111"}
			config := map[string]any{protocol: credential}
			if protocol == "mixed" {
				config["socks"], config["http"] = credential, credential
			}
			options := map[string]any{}
			if protocol == "shadowsocks" {
				options["method"] = "aes-128-gcm"
			}
			inbound := wave4Inbound(t, protocol, "[2001:db8::1]", options, map[string]any{})
			links, err := Generate(wave4JSON(t, config), inbound, "unused.example")
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if protocol == "naive" {
				wantCount = 3
			}
			if protocol == "mixed" {
				wantCount = 2
			}
			if len(links) != wantCount {
				t.Fatalf("link count=%d want=%d", len(links), wantCount)
			}
			for _, link := range links {
				parsed, _, err := Parse(link, 0)
				if err != nil {
					t.Fatal(err)
				}
				if (*parsed)["server"] != "2001:db8::1" || (*parsed)["server_port"] != 443 {
					t.Fatal("endpoint changed in round trip")
				}
				key := "password"
				if protocol == "hysteria" {
					key = "auth_str"
				}
				if (*parsed)[key] != password {
					t.Fatal("password/auth did not survive round trip")
				}
				if protocol == "socks" || protocol == "http" || protocol == "mixed" || protocol == "naive" {
					if (*parsed)["username"] != username {
						t.Fatal("username did not survive round trip")
					}
				}
			}
		})
	}
}

func TestWave4NaiveVariantsAndTrustDiagnostic(t *testing.T) {
	config := wave4JSON(t, map[string]any{"naive": map[string]any{"username": "fixture", "password": "p@:/?#%雪"}})
	for _, tc := range []struct {
		network any
		schemes []string
	}{{nil, []string{"http2", "naive+https", "naive+quic"}}, {"tcp", []string{"http2", "naive+https"}}, {"udp", []string{"naive+quic"}}, {[]string{"udp", "tcp"}, []string{"http2", "naive+https", "naive+quic"}}} {
		inbound := wave4Inbound(t, "naive", "example.com", map[string]any{"network": tc.network, "tcp_fast_open": true}, nil)
		links, err := Generate(config, inbound, "")
		if err != nil {
			t.Fatal(err)
		}
		var schemes []string
		for _, link := range links {
			u, _ := url.Parse(link)
			schemes = append(schemes, u.Scheme)
			parsed, _, err := Parse(link, 0)
			if err != nil || (*parsed)["tcp_fast_open"] != true {
				t.Fatal("Naive option lost")
			}
		}
		if !reflect.DeepEqual(schemes, tc.schemes) {
			t.Fatalf("schemes=%v want=%v", schemes, tc.schemes)
		}
	}
	inbound := wave4Inbound(t, "naive", "example.com", nil, nil)
	inbound.Addrs = wave4JSON(t, []map[string]any{{"server": "example.com", "server_port": 443, "tls": map[string]any{"certificate_public_key_sha256": []string{"fixture-pin"}}}})
	_, err := Generate(config, inbound, "")
	var unsupported *UnsupportedProjection
	if !errors.As(err, &unsupported) || strings.Contains(err.Error(), "fixture-pin") {
		t.Fatal("certificate trust must be explicitly diagnosed without exposing its value")
	}
	// Legacy standard-base64 links remain readable even when their payload has '/'.
	payload := base64.StdEncoding.EncodeToString([]byte("fixture:p@:/?%雪@example.com:443"))
	if _, _, err := Parse("http2://"+payload+"#fixture", 0); err != nil {
		t.Fatal(err)
	}
}

func TestWave4ShadowsocksPluginAnd2022Userinfo(t *testing.T) {
	options := `mode=websocket;host=雪.example;path=/a\;b\=c\\d;tls`
	inbound := wave4Inbound(t, "shadowsocks", "2001:db8::1", map[string]any{"method": "aes-128-gcm"}, map[string]any{"plugin": "v2ray-plugin", "plugin_opts": options})
	links, err := Generate(json.RawMessage(`{"shadowsocks":{"password":"p@:/?#% 雪"}}`), inbound, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(links[0])
	if u.Path != "/" || u.Query().Get("plugin") != "v2ray-plugin;"+options {
		t.Fatal("SIP002 plugin shape or option order changed")
	}
	parsed, _, err := Parse(links[0], 0)
	if err != nil || (*parsed)["plugin_opts"] != options {
		t.Fatal("plugin round trip failed")
	}
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	clientKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	inbound.Options = wave4JSON(t, map[string]any{"method": "2022-blake3-aes-128-gcm", "password": serverKey})
	links, err = Generate(wave4JSON(t, map[string]any{"shadowsocks16": map[string]any{"password": clientKey}}), inbound, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(links[0])
	pass, present := u.User.Password()
	if u.User.Username() != "2022-blake3-aes-128-gcm" || !present || pass != serverKey+":"+clientKey {
		t.Fatal("AEAD2022 must use structured plaintext userinfo")
	}
	for _, metadata := range []map[string]any{{"plugin_opts": "tls"}, {"plugin": 7}, {"plugin": "v2ray-plugin", "plugin_opts": true}, {"plugin": "invalid;plugin"}, {"plugin": "v2ray-plugin", "plugin_opts": `bad\q`}} {
		inbound.OutJson = wave4JSON(t, metadata)
		if _, err := Generate(wave4JSON(t, map[string]any{"shadowsocks16": map[string]any{"password": clientKey}}), inbound, ""); err == nil {
			t.Fatal("malformed plugin metadata accepted")
		}
	}
}

func TestWave4WebSocketEarlyDataRoundTrip(t *testing.T) {
	config := json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`)
	path := "/雪?token=a%26b&empty=&literal=%3F"
	for _, max := range []int{0, 2048, 8192} {
		inbound := wave4Inbound(t, "vless", "example.com", map[string]any{"transport": map[string]any{"type": "ws", "path": path, "max_early_data": max, "early_data_header_name": "Sec-WebSocket-Protocol"}}, nil)
		links, err := Generate(config, inbound, "")
		if err != nil {
			t.Fatal(err)
		}
		parsed, _, err := Parse(links[0], 0)
		if err != nil {
			t.Fatal(err)
		}
		transport := (*parsed)["transport"].(map[string]any)
		if transport["path"] != path {
			t.Fatal("WS path/query changed")
		}
		if max > 0 && transport["max_early_data"] != uint32(max) {
			t.Fatal("WS early data lost")
		}
	}
	inbound := wave4Inbound(t, "vless", "example.com", map[string]any{"transport": map[string]any{"type": "ws", "path": "/", "max_early_data": 1024, "early_data_header_name": "X-Custom"}}, nil)
	_, err := Generate(config, inbound, "")
	var unsupported *UnsupportedProjection
	if !errors.As(err, &unsupported) {
		t.Fatal("custom header needs an explicit delivery diagnosis")
	}
}

func TestWave4HysteriaRangeRoundTripAndUnsupportedInterval(t *testing.T) {
	for _, protocol := range []string{"hysteria", "hysteria2"} {
		inbound := wave4Inbound(t, protocol, "example.com", nil, map[string]any{"server_ports": []any{"443", "8443:8445", "65535:65535"}})
		config := wave4JSON(t, map[string]any{protocol: map[string]any{"password": "fixture", "auth_str": "fixture"}})
		links, err := Generate(config, inbound, "")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(links[0])
		if u.Query().Get("mport") != "443,8443:8445,65535" {
			t.Fatal("range share grammar changed")
		}
		parsed, _, err := Parse(links[0], 0)
		if err != nil || !reflect.DeepEqual((*parsed)["server_ports"], []string{"443:443", "8443:8445", "65535:65535"}) {
			t.Fatal("range round trip lost segments")
		}
		inbound.OutJson = wave4JSON(t, map[string]any{"server_ports": []string{"443:444"}, "hop_interval": "30s"})
		_, err = Generate(config, inbound, "")
		var unsupported *UnsupportedProjection
		if !errors.As(err, &unsupported) {
			t.Fatal("hop interval was silently dropped")
		}
	}
}

func TestWave4MalformedLinksFailWithoutCredentials(t *testing.T) {
	for _, link := range []string{"trojan://fixture:secret@example.com:443", "anytls://fixture:secret@example.com:443", "tuic://fixture@example.com:443", "ss://broken@example.com:443", "naive+https://u%3Ainvalid:fixture@example.com:443", "trojan://fixture@[2001:db8::1]:0", "trojan://fixture@2001:db8::1:443", "trojan://fixture@example.com:", "trojan://fixture@[2001:db8::1:443", "hysteria2://fixture@example.com:443?mport=2-1", "vless://fixture@example.com:443?type=ws&path=%2F%3Fed%3D0", "trojan://fixture@example.com:443?sni=one&sni=two", "trojan://fixture@example.com:443?sni=%ff"} {
		if _, _, err := Parse(link, 0); err == nil {
			t.Fatal("malformed shape accepted")
		} else if strings.Contains(err.Error(), "fixture") || strings.Contains(err.Error(), "secret") {
			t.Fatal("malformed error reflected credentials")
		}
	}
}

func TestWave4PublicRemarkNamesAreDeterministic(t *testing.T) {
	inbound := wave4Inbound(t, "trojan", "example.com", nil, nil)
	inbound.Addrs = wave4JSON(t, []map[string]any{{"server": "a.example", "server_port": 443}, {"server": "b.example", "server_port": 443}, {"server": "c.example", "server_port": 443, "remark": "-2"}})
	config := wave4JSON(t, map[string]any{"trojan": map[string]any{"password": "fixture"}, canonical.MetadataKey: map[string]any{canonical.PublicRemarkKey: "Public"}})
	first, err := Generate(config, inbound, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(config, inbound, "")
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("repeated generation changed output")
	}
	var names []string
	for _, link := range first {
		u, _ := url.Parse(link)
		names = append(names, u.Fragment)
	}
	if !reflect.DeepEqual(names, []string{"Public", "Public-3", "Public-2"}) {
		t.Fatalf("collision names=%v", names)
	}
}
