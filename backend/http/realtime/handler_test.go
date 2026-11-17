package realtimehttp

import (
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"testing"
)

func TestRealtimeTicketBindingRequiresExactRequestSession(t *testing.T) {
	base := panelsecurity.RealtimeSessionBinding{
		UserID: 7, Username: "admin", SessionRef: "session-a",
		SessionGenerationRevision: "generation-a", CredentialGeneration: 3, MFAGeneration: 5,
	}
	if !sameRealtimeSessionBinding(base, base) {
		t.Fatal("exact realtime ticket/request session binding was rejected")
	}
	mutations := map[string]func(*panelsecurity.RealtimeSessionBinding){
		"user":     func(binding *panelsecurity.RealtimeSessionBinding) { binding.UserID++ },
		"username": func(binding *panelsecurity.RealtimeSessionBinding) { binding.Username = "other" },
		"session":  func(binding *panelsecurity.RealtimeSessionBinding) { binding.SessionRef = "session-b" },
		"global generation": func(binding *panelsecurity.RealtimeSessionBinding) {
			binding.SessionGenerationRevision = "generation-b"
		},
		"credential generation": func(binding *panelsecurity.RealtimeSessionBinding) { binding.CredentialGeneration++ },
		"MFA generation":        func(binding *panelsecurity.RealtimeSessionBinding) { binding.MFAGeneration++ },
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
	if sameRealtimeSessionBinding(panelsecurity.RealtimeSessionBinding{Username: "admin"}, panelsecurity.RealtimeSessionBinding{Username: "admin"}) {
		t.Fatal("legacy username-only ticket binding matched a request session")
	}
}
