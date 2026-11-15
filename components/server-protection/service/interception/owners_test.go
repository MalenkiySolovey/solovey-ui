package interception

import "testing"

func TestMissingResourceOwnerDoesNotConsultOtherOwners(t *testing.T) {
	for _, missing := range []string{"interceptions", "ingress"} {
		t.Run(missing, func(t *testing.T) {
			s := New(struct{ InterceptionSource }{}, struct{ IngressScopeSource }{}, struct{ ProbeInventory }{})
			if missing == "interceptions" {
				s.Interceptions = nil
			} else {
				s.IngressScopes = nil
			}
			status, err := s.Status(t.Context())
			if err == nil || status.MutationAvailable || status.DefaultEnabled {
				t.Fatalf("missing owner admitted: %#v %v", status, err)
			}
		})
	}
}
