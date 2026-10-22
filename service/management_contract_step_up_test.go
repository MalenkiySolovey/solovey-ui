package service

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
)

func TestManagementContractVectorsPreserveExistingStepUpBindings(t *testing.T) {
	settings := initSettingTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../contracts/management/v1/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			ID, Kind, ActorRef string
			Now                int64
			Grants             []wire.ManagementGrant
		}
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"accepted-owner-intent": true, "wrong-grant-actor": true, "wrong-grant-action": true, "wrong-grant-input-digest": true, "stale-authorization-generation": true, "stale-credential-generation": true, "stale-mfa-generation": true, "expired-grant": true}
	count := 0
	for _, v := range set.Cases {
		if !selected[v.ID] {
			continue
		}
		count++
		t.Run(v.ID, func(t *testing.T) {
			now := time.Unix(v.Now, 0)
			stepUp := StepUpService{SettingService: *settings, Now: func() time.Time { return now }}
			// The browser's client/proxy identity remains private authority
			// context; it is neither transmitted nor reconstructed from grant JSON.
			binding := StepUpBinding{UserID: 1, SessionRef: "session:fixture", SessionGenerationRevision: "3", CredentialGeneration: 4, MFAGeneration: 5, ClientIdentityRevision: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", OperationKind: "backup.restore", TargetDigest: wire.OwnerInputDigest("solovey.action/backup.restore/v1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Assurance: AssuranceMFA}
			issued, err := stepUp.Issue(binding)
			if err != nil {
				t.Fatal(err)
			}
			g := v.Grants[0]
			changed := binding
			if g.ActorRef != v.ActorRef {
				changed.UserID = 2
			}
			changed.OperationKind = g.ActionID
			changed.TargetDigest = string(g.InputDigest)
			changed.SessionGenerationRevision = string(g.AuthorizationGeneration)
			changed.CredentialGeneration, err = strconv.ParseUint(string(g.CredentialGeneration), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			changed.MFAGeneration, err = strconv.ParseUint(string(g.MfaGeneration), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if v.ID == "expired-grant" {
				now = now.Add(StepUpTTL)
			}
			err = stepUp.Consume(issued.Token, changed)
			if (err == nil) != (v.ID == "accepted-owner-intent") {
				t.Fatalf("binding result: %v", err)
			}
			if v.ID != "expired-grant" && v.ID != "accepted-owner-intent" {
				if err := stepUp.Consume(issued.Token, binding); err != nil {
					t.Fatalf("mismatch consumed valid one-use authority: %v", err)
				}
			}
			if err := stepUp.Consume(issued.Token, binding); err == nil {
				t.Fatal("consumed/expired grant accepted again")
			}
		})
	}
	if count != len(selected) {
		t.Fatalf("missing StepUp source vectors: %d", count)
	}
}
