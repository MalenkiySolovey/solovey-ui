//go:build !minimal

package api

import (
	"context"
	"testing"
	"time"

	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestLegacyGraylistCannotBypassDecisionActionMaterialization(t *testing.T) {
	_, _, _, repository, db := newProtectionAPIRouterWithDB(t, readScope, protectionfronting.NewNginxAdapter())
	now := time.Unix(1000, 0).UTC()
	row := protectionrepository.GraylistModel{ResourceID: "core:inbound:one", IPCIDR: "203.0.113.10/32", IPFamily: 4, Score: 100, Reason: "legacy", LastSignal: "legacy", ExpiresAt: now.Add(time.Hour).Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := db.WithContext(context.Background()).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	_, actions, err := (Handler{deps: Deps{Repository: repository}}).firewallBaselinePolicyInputs(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("legacy graylist row materialized without a validated AppliedActionV1: %#v", actions)
	}
}

func TestInvalidTrustedSourceFailsClosed(t *testing.T) {
	_, _, _, repository, db := newProtectionAPIRouterWithDB(t, readScope, protectionfronting.NewNginxAdapter())
	now := time.Unix(1000, 0).UTC()
	row := protectionrepository.IPAllowlistModel{IPCIDR: "not-a-prefix", Reason: "corrupt legacy row", CreatedBy: "fixture", CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := db.WithContext(context.Background()).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Handler{deps: Deps{Repository: repository}}).firewallBaselinePolicyInputs(context.Background(), now); err == nil {
		t.Fatal("invalid active trusted source was ignored instead of blocking planning")
	}
}
