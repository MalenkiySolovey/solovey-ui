package service

import (
	"context"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	opsdoctor "github.com/MalenkiySolovey/solovey-ui/internal/ops/doctor"
)

type doctorPanelProof struct{ at time.Time }

func (doctorPanelProof) ResourceID() string { return "core:panel:web" }
func (c doctorPanelProof) Check(context.Context) componenthealth.Result {
	return componenthealth.WithEvidence(componenthealth.Result{ResourceID: c.ResourceID(), Status: componenthealth.StatusOK, Check: "panel_listener", FactCode: "listener_ready"},
		"core", componenthealth.RuntimeReady, "panel_listener", componenthealth.Revision(configidentity.GetVersion()), c.at, 30*time.Second)
}

func TestDoctorConsumesFreshEvidenceWithoutPromotingItsProposition(t *testing.T) {
	initDoctorTestDB(t)
	if err := (&SettingService{}).SetConfig(`{"log":{"disabled":true},"route":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		at       time.Time
		severity opsdoctor.Severity
	}{
		{time.Now(), opsdoctor.SeverityOK}, {time.Now().Add(-10 * time.Second), opsdoctor.SeverityWarn},
	} {
		stop, err := componenthealth.Register(doctorPanelProof{test.at})
		if err != nil {
			t.Fatal(err)
		}
		report := (&DoctorService{}).Run("localhost")
		stop()
		if !doctorReportHas(report, "panel-runtime-evidence", test.severity) {
			t.Fatalf("Doctor lost proof/freshness: %+v", report.Items)
		}
		for _, item := range report.Items {
			if item.ID != "panel-runtime-evidence" {
				continue
			}
			result, ok := item.Details.(componenthealth.Result)
			if !ok || result.Evidence == nil || result.Evidence.ProofKind != componenthealth.RuntimeReady || result.Evidence.DesiredRevision == "" {
				t.Fatalf("Doctor proof metadata=%+v", item.Details)
			}
		}
	}
}
