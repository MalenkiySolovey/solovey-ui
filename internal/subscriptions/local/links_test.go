package local

import (
	"encoding/json"
	"reflect"
	"testing"

	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
)

func TestResolveClientLinksExternalModeSkipsLocal(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"local","uri":"vless://local#local"},
		{"type":"external","uri":"trojan://external#external"},
		{"type":"sub","uri":"https://example.com/sub"}
	]`)
	got := ResolveClientLinksWithFetcher(raw, LinkModeExternal, " info", func(rawURL string) (string, error) {
		return "vless://sub-a#sub-a\nvless://sub-b#sub-b", nil
	})
	want := []string{"trojan://external#external", "vless://sub-a#sub-a", "vless://sub-b#sub-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links = %#v", got)
	}
}

func TestResolveClientLinksAllModeAddsClientInfo(t *testing.T) {
	raw := json.RawMessage(`[{"type":"local","uri":"vless://local#local"}]`)
	got := ResolveClientLinksWithFetcher(raw, LinkModeAll, " info", nil)
	if !reflect.DeepEqual(got, []string{"vless://local#local info"}) {
		t.Fatalf("links = %#v", got)
	}
}

func TestAddClientInfoUpdatesVmessRemark(t *testing.T) {
	raw := map[string]interface{}{"ps": "node", "add": "example.com"}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := AddClientInfo("vmess://"+uricodec.Encode(data), " info")
	outbound, _, err := suburi.Parse(got, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tag := (*outbound)["tag"]; tag != "node info" {
		t.Fatalf("tag = %#v", tag)
	}
}
