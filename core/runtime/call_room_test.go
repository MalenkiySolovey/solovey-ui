package runtime

import (
	"context"
	"strings"
	"testing"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/sagernet/sing-box/adapter"
)

type callRoomInboundFixture struct{ adapter.Inbound }

func (*callRoomInboundFixture) Type() string { return "call" }
func (*callRoomInboundFixture) Tag() string  { return "private-room" }
func (*callRoomInboundFixture) CallJoinLink() (string, bool) {
	return "https://fixture.invalid/private-room-canary", true
}

type callRoomManagerFixture struct{ adapter.InboundManager }

func (*callRoomManagerFixture) Get(tag string) (adapter.Inbound, bool) {
	return &callRoomInboundFixture{}, tag == "private-room"
}

func (*callRoomManagerFixture) Inbounds() []adapter.Inbound {
	return []adapter.Inbound{&callRoomInboundFixture{}}
}

func TestCallRoomDeliveryRequiresCurrentGenerationAndExactInbound(t *testing.T) {
	core := NewCore()
	core.isRunning, core.instance = true, &corebox.Box{}
	core.inboundManager = &callRoomManagerFixture{}
	core.privateAPI = &privateAPI{generation: "current-generation"}
	digest := strings.Repeat("a", 64)
	core.effectiveInbounds["private-room"] = InboundRuntimeRecord{
		Tag: "private-room", Type: "call", OptionsDigest: digest, ManagerGeneration: 1,
	}
	var borrowed []byte
	deliveries := 0
	use := func(value []byte) error {
		deliveries++
		borrowed = value
		if string(value) != "https://fixture.invalid/private-room-canary" {
			t.Fatal("trusted operation lost the current room")
		}
		return nil
	}
	if err := core.UseCallRoom(t.Context(), "current-generation", "private-room", digest, use); err != nil || deliveries != 1 {
		t.Fatal("current private room was unavailable", err)
	}
	if strings.Trim(string(borrowed), "\x00") != "" {
		t.Fatal("borrowed private room was retained after the callback")
	}
	for _, target := range [][3]string{
		{"old-generation", "private-room", digest},
		{"current-generation", "private-room", strings.Repeat("b", 64)},
		{"current-generation", "missing", digest},
	} {
		if err := core.UseCallRoom(context.Background(), target[0], target[1], target[2], use); err == nil || strings.Contains(err.Error(), "canary") {
			t.Fatal("stale/mismatched target acquired or disclosed a room")
		}
	}
	if deliveries != 1 {
		t.Fatal("rejected room operation reached its private callback")
	}
}
