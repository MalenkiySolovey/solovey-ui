package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentMemoryBytesRejectsWrappingNegativeInput(t *testing.T) {
	for _, consumer := range [][2]string{{"hysteria2", "outbound"}, {"hysteria", "inbound"}, {"naive", "outbound"}} {
		if findings := CurrentFindings(consumer[0], consumer[1], "entity", json.RawMessage(`{"stream_receive_window":-1}`)); len(findings) == 0 || findings[0].Code != "PROTOCOL_SIZE_INVALID" {
			t.Fatal("negative byte quantity wrapped into the current consumer")
		}
		for _, value := range []string{"0", `"1MB"`} {
			if findings := CurrentFindings(consumer[0], consumer[1], "entity", json.RawMessage(`{"stream_receive_window":`+value+`}`)); len(findings) != 0 {
				t.Fatal("zero or pinned binary unit string was rejected")
			}
		}
	}
}

func TestAcceptedOldProtocolReplay(t *testing.T) {
	var manifest []struct {
		ID     string `json:"fixture_id"`
		Hash   string `json:"config_sha256"`
		Commit string `json:"solovey_commit"`
		Core   string `json:"core_version"`
		Side   string `json:"side"`
	}
	raw, err := os.ReadFile("testdata/accepted-old/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range manifest {
		t.Run(fixture.ID, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata/accepted-old", fixture.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(source)
			if hex.EncodeToString(hash[:]) != fixture.Hash || fixture.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || fixture.Core != "v1.13.18" {
				t.Fatal("old capture identity changed")
			}
			var header struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(source, &header); err != nil {
				t.Fatal(err)
			}
			side := fixture.Side
			if side == "in" {
				side = "inbound"
			}
			if side == "out" {
				side = "outbound"
			}
			prepared, err := PrepareUpgrade(header.Type, side, "fixture", source)
			if err != nil {
				t.Fatal(err)
			}
			if findings := CurrentFindings(header.Type, side, "fixture", prepared.Candidate); len(findings) > 0 {
				t.Fatalf("candidate rejected at owner: %s", findings[0].Code)
			}
			repeated, err := PrepareUpgrade(header.Type, side, "fixture", prepared.Candidate)
			if err != nil || !bytes.Equal(repeated.Candidate, prepared.Candidate) {
				t.Fatal("repeat upgrade changed candidate")
			}
			var before, after map[string]json.RawMessage
			_ = json.Unmarshal(source, &before)
			_ = json.Unmarshal(prepared.Candidate, &after)
			if header.Type == "hysteria" {
				for _, pair := range [][2]string{{"recv_window_conn", "connection_receive_window"}, {"recv_window", "stream_receive_window"}, {"recv_window_client", "stream_receive_window"}, {"max_conn_client", "max_concurrent_streams"}, {"disable_mtu_discovery", "disable_path_mtu_discovery"}} {
					if original, present := before[pair[0]]; present {
						if !equalJSON(original, after[pair[1]]) {
							t.Fatal("zero/false/size/stream value changed")
						}
						if _, present := after[pair[0]]; present {
							t.Fatal("legacy duplicate remains")
						}
					}
				}
			}
			if header.Type == "hysteria2" {
				_, present := after["disable_chrome_parrot"]
				if present != (side == "outbound") {
					t.Fatal("handshake compatibility applied to wrong consumer")
				}
			}
		})
	}
}

func TestProtocolConflictPreservesWholeCandidate(t *testing.T) {
	source := []byte(`{"outbounds":[{"type":"hysteria2","tag":"old"},{"type":"hysteria","recv_window_conn":0,"connection_receive_window":"1MB"}],"dns":{"servers":[]}}`)
	projection, err := PrepareConfigUpgrade(source)
	if err == nil || !bytes.Equal(projection.Candidate, source) || !strings.Contains(err.Error(), "TRANSPORT_ALIAS_CONFLICT") {
		t.Fatal("failed later entity published earlier conversion")
	}
}

func TestCurrentProtocolSizeUnitsAndHandshakeIntent(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`{"stream_receive_window":"1MB","connection_receive_window":0,"disable_path_mtu_discovery":false,"max_concurrent_streams":0}`), json.RawMessage(`{"stream_receive_window":0,"initial_packet_size":1200,"idle_timeout":"0s","keep_alive_period":"15s"}`)} {
		if findings := CurrentFindings("hysteria", "inbound", "fixture", raw); len(findings) > 0 {
			t.Fatal("valid units/zero/false rejected")
		}
	}
	if findings := CurrentFindings("hysteria2", "outbound", "fixture", json.RawMessage(`{"stream_receive_window":"1MB","disable_chrome_parrot":false}`)); len(findings) > 0 {
		t.Fatal("explicit current handshake intent rejected")
	}
	for _, source := range []json.RawMessage{json.RawMessage(`{"disable_chrome_parrot":false}`), json.RawMessage(`{"disable_chrome_parrot":true}`)} {
		prepared, err := PrepareUpgrade("hysteria2", "outbound", "fixture", source)
		if err != nil || !bytes.Equal(prepared.Candidate, source) {
			t.Fatal("explicit handshake choice overwritten")
		}
	}
	if findings := CurrentFindings("hysteria", "outbound", "fixture", json.RawMessage(`{"stream_receive_window":"bogus-unit"}`)); len(findings) == 0 {
		t.Fatal("invalid size admitted")
	}
	untouched := []byte(`{"outbounds":[{"type":"vmess","transport":{"type":"grpc","ping_timeout":"15s"}}]}`)
	prepared, err := PrepareConfigUpgrade(untouched)
	if err != nil || !bytes.Equal(prepared.Candidate, untouched) {
		t.Fatal("unrelated per-consumer ping field renamed")
	}
}
