package web

import (
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
)

func TestRunningPanelPublishesOnlyFreshListenerProposition(t *testing.T) {
	db := initWebRouterTestDB(t)
	setWebRouterSetting(t, db, "webListen", "127.0.0.1")
	setWebRouterSetting(t, db, "webPort", "0")
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	request := componenthealth.Requirement{Owner: "core", Source: "panel_listener", ProofKind: componenthealth.RuntimeReady,
		DesiredRevision: componenthealth.Revision(configidentity.GetVersion()), MaxAge: 5 * time.Second}
	result := componenthealth.Require(t.Context(), "core:panel:web", request)
	if result.Status != componenthealth.StatusOK || result.Evidence == nil || result.Evidence.EvidenceRef == "" {
		t.Fatalf("real listener proof=%+v", result)
	}
	request.ProofKind = componenthealth.DataPlaneVerified
	if result := componenthealth.Require(t.Context(), "core:panel:web", request); result.Status == componenthealth.StatusOK {
		t.Fatal("listener promoted to data-plane proof")
	}
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if result := componenthealth.Check(t.Context(), "core:panel:web"); result.Status == componenthealth.StatusOK {
		t.Fatal("retired listener remained ready")
	}
}
