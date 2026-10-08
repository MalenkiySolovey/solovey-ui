package entityinbounds

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestNaiveAndHysteria2RegenerationPreservesCustomIntent(t *testing.T) {
	for _, congestion := range []string{"", "bbr", "bbr2", "custom"} {
		t.Run("naive-"+congestion, func(t *testing.T) {
			options := map[string]any{}
			if congestion != "" {
				options["quic_congestion_control"] = congestion
			}
			raw, _ := json.Marshal(options)
			inbound := model.Inbound{Type: "naive", Tag: "fixture", Addrs: json.RawMessage(`[{"addr":"127.0.0.1","port":443}]`), Options: raw, OutJson: json.RawMessage(`{"quic":true,"quic_congestion_control":"bbr2","custom_extension":{"intent":false},"extra_headers":{"x-fixture":"retained"}}`)}
			if err := FillOutboundJSON(&inbound, "fixture.invalid"); err != nil {
				t.Fatal(err)
			}
			var generated map[string]json.RawMessage
			_ = json.Unmarshal(inbound.OutJson, &generated)
			if _, present := generated["custom_extension"]; !present {
				t.Fatal("non-derived custom field lost")
			}
			if _, present := generated["extra_headers"]; !present {
				t.Fatal("non-derived headers lost")
			}
			if congestion == "" {
				if _, present := generated["quic"]; present {
					t.Fatal("stale derived QUIC flag remains")
				}
				if _, present := generated["quic_congestion_control"]; present {
					t.Fatal("stale derived congestion remains")
				}
			}
			first := append([]byte(nil), inbound.OutJson...)
			if err := FillOutboundJSON(&inbound, "fixture.invalid"); err != nil || !bytes.Equal(first, inbound.OutJson) {
				t.Fatal("repeat generation changed output")
			}
		})
	}
	for _, intent := range []string{"", `,"disable_chrome_parrot":false`} {
		inbound := model.Inbound{Type: "hysteria2", Tag: "fixture", Addrs: json.RawMessage(`[{"addr":"127.0.0.1","port":443}]`), Options: json.RawMessage(`{}`), OutJson: json.RawMessage(`{"custom_extension":false` + intent + `}`)}
		if err := FillOutboundJSON(&inbound, "fixture.invalid"); err != nil {
			t.Fatal(err)
		}
		var generated map[string]json.RawMessage
		_ = json.Unmarshal(inbound.OutJson, &generated)
		want := "true"
		if intent != "" {
			want = "false"
		}
		if string(generated["disable_chrome_parrot"]) != want {
			t.Fatal("generated handshake behavior changed explicit intent")
		}
	}
}

func TestGeneratedTLSClientKeepsPairPinAndTimeout(t *testing.T) {
	inbound := model.Inbound{Type: "trojan", Tag: "fixture", TlsId: 1, Tls: &model.Tls{Server: json.RawMessage(`{"enabled":true,"handshake_timeout":"0s"}`), Client: json.RawMessage(`{"insecure":false,"client_certificate_path":"fixture.crt","client_key_path":"fixture.key","certificate_public_key_sha256":["fixture-pin"]}`)}, Addrs: json.RawMessage(`[]`), Options: json.RawMessage(`{}`)}
	if err := FillOutboundJSON(&inbound, "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	var generated struct {
		TLS map[string]json.RawMessage `json:"tls"`
	}
	_ = json.Unmarshal(inbound.OutJson, &generated)
	for _, key := range []string{"handshake_timeout", "client_certificate_path", "client_key_path", "certificate_public_key_sha256", "insecure"} {
		if _, present := generated.TLS[key]; !present {
			t.Fatal("TLS client field lost: " + key)
		}
	}
}
