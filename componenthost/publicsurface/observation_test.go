package publicsurface

import "testing"

func TestObservationRegistryIsNonBlockingAndCountsDrops(t *testing.T) {
	registry := newObservationRegistry()
	subscription, unregister, err := registry.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	observation := Observation{ResourceID: "resource-one", ResourceKind: "public_site", PathClass: "scanner_path", MethodClass: "get", StatusClass: "4xx", UserAgentClass: "ua_scanner", BytesClass: "small", DurationClass: "fast"}
	if accepted, dropped := registry.Emit(observation); accepted != 1 || dropped != 0 {
		t.Fatalf("first emit = %d/%d", accepted, dropped)
	}
	if accepted, dropped := registry.Emit(observation); accepted != 0 || dropped != 1 {
		t.Fatalf("second emit = %d/%d", accepted, dropped)
	}
	if subscription.Dropped() != 1 || subscription.Pending() != 1 {
		t.Fatalf("subscription counters = dropped:%d pending:%d", subscription.Dropped(), subscription.Pending())
	}
	unregister()
	if accepted, dropped := registry.Emit(observation); accepted != 0 || dropped != 0 {
		t.Fatalf("emit without subscribers = %d/%d", accepted, dropped)
	}
}

func TestPublicClassifiersNeverReturnRawInput(t *testing.T) {
	if got := ClassifyPath("/wp-admin/?token=secret", false); got != "scanner_path" {
		t.Fatalf("path class = %q", got)
	}
	if got := ClassifyUserAgent("curl/8.0 secret"); got != "ua_scanner" {
		t.Fatalf("UA class = %q", got)
	}
	if got := ClassifyPath("/docs?uuid=secret", false); got != "fallback_path" {
		t.Fatalf("fallback class = %q", got)
	}
}

func TestObservationSubscriptionBufferIsBounded(t *testing.T) {
	registry := newObservationRegistry()
	subscription, unregister, err := registry.Subscribe(maxObservationBuffer * 100)
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	if capacity := cap(subscription.channel); capacity != maxObservationBuffer {
		t.Fatalf("subscription buffer capacity = %d", capacity)
	}
}

func TestObservationSubscribersAreBounded(t *testing.T) {
	registry := newObservationRegistry()
	for index := 0; index < maxObservationSubscribers; index++ {
		if _, _, err := registry.Subscribe(1); err != nil {
			t.Fatalf("subscriber %d: %v", index, err)
		}
	}
	if _, _, err := registry.Subscribe(1); err == nil {
		t.Fatal("observation registry accepted an unbounded subscriber")
	}
}
