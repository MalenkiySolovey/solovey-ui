package manifest

import "testing"

func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		ID:       "remote-subscriptions",
		Name:     "Remote Subscriptions",
		Delivery: DeliveryInProcess,
		Frontend: Frontend{
			Entries: []string{
				"frontend/views/RemoteOutboundSubscriptions.vue",
				"frontend/locales/en/telegram.ts",
			},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest failed: %v", err)
	}

	for _, row := range []Manifest{
		{ID: "Bad_ID", Name: "Bad", Delivery: DeliveryInProcess},
		{ID: "bad", Delivery: DeliveryInProcess},
		{ID: "bad", Name: "Bad"},
		{ID: "bad", Name: "Bad", Delivery: Delivery("sidecar")},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{""}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"../views/Bad.vue"}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"src/views/Bad.js"}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"assets/Bad.vue"}}},
	} {
		if err := row.Validate(); err == nil {
			t.Fatalf("manifest %#v unexpectedly validated", row)
		}
	}
}
