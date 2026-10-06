package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
)

func TestManualProbeProjectsThroughLoadAndRealtimeWithoutSnapshotMutation(t *testing.T) {
	initSettingTestDB(t)
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := core.Stop(); err != nil {
			t.Error(err)
		}
	})
	runtime := NewRuntime(core)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	config := ConfigService{Runtime: runtime}
	if result := config.CheckOutbound("direct", server.URL); !result.OK {
		t.Fatalf("manual probe = %#v", result)
	}
	stats := StatsService{Runtime: runtime}
	online, err := stats.GetOnlines()
	if err != nil || online.OutboundHealth["direct"].Source != coreruntime.ProbeSourceManual {
		t.Fatalf("load projection = %#v / %v", online, err)
	}
	delete(online.OutboundHealth, "direct")
	if len(core.OutboundHealthSnapshot()) != 1 {
		t.Fatal("consumer mutated probe owner")
	}
	realtime.CloseAll("test_reset")
	t.Cleanup(func() { realtime.CloseAll("test_done") })
	ch := make(chan realtime.Event, 2)
	unregister := realtime.Register(&realtime.ClientHandle{User: "admin", Scope: realtime.ScopeAdmin, SendCh: ch})
	defer unregister()
	if err := stats.saveStatsSamples(false, nil); err != nil {
		t.Fatal(err)
	}
	event := expectRealtimeEvent(t, ch, realtime.TopicOnlines)
	payload, ok := event.Payload.(onlines)
	if !ok || payload.OutboundHealth["direct"].Status != "healthy" {
		t.Fatalf("realtime projection = %#v", event.Payload)
	}
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	online, err = stats.GetOnlines()
	if err != nil || len(online.OutboundHealth) != 0 {
		t.Fatal("stopped runtime projected historical health")
	}
}
