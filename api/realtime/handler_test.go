package realtimehttp

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestRealtimeTicketBindingRequiresExactRequestSession(t *testing.T) {
	base := service.RealtimeSessionBinding{
		UserID: 7, Username: "admin", SessionRef: "session-a",
		SessionGenerationRevision: "generation-a", CredentialGeneration: 3, MFAGeneration: 5,
	}
	if !sameRealtimeSessionBinding(base, base) {
		t.Fatal("exact realtime ticket/request session binding was rejected")
	}
	mutations := map[string]func(*service.RealtimeSessionBinding){
		"user":                  func(binding *service.RealtimeSessionBinding) { binding.UserID++ },
		"username":              func(binding *service.RealtimeSessionBinding) { binding.Username = "other" },
		"session":               func(binding *service.RealtimeSessionBinding) { binding.SessionRef = "session-b" },
		"global generation":     func(binding *service.RealtimeSessionBinding) { binding.SessionGenerationRevision = "generation-b" },
		"credential generation": func(binding *service.RealtimeSessionBinding) { binding.CredentialGeneration++ },
		"MFA generation":        func(binding *service.RealtimeSessionBinding) { binding.MFAGeneration++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if sameRealtimeSessionBinding(base, changed) {
				t.Fatal("foreign realtime request session matched the ticket binding")
			}
		})
	}
	if sameRealtimeSessionBinding(service.RealtimeSessionBinding{Username: "admin"}, service.RealtimeSessionBinding{Username: "admin"}) {
		t.Fatal("legacy username-only ticket binding matched a request session")
	}
}
