package requestbudget

import (
	"net/http"
	"testing"
)

func TestSSHManagementRoutesUseTinyBodiesAndOneGlobalCandidateLane(t *testing.T) {
	for _, test := range []struct {
		path, action, stepUp string
	}{
		{"/api/v1/operations/ssh/candidate", "ssh_candidate_apply", "ssh.candidate.apply"},
		{"/api/v1/operations/ssh/candidate/:operationId/reconnect/confirm", "ssh_candidate_confirm", "ssh.candidate.confirm"},
		{"/api/v1/operations/ssh/candidate/:operationId/rollback", "ssh_candidate_rollback", "ssh.candidate.rollback"},
	} {
		policy := classify(http.MethodPost, test.path)
		if policy.ActionScope != test.action || policy.StepUpOperation != test.stepUp || policy.BodyClass != BodyAuthTiny || policy.MaxBodyBytes != AuthTinyBytes || policy.ConcurrencyClass != "ssh_candidate" {
			t.Fatalf("%s policy=%#v", test.path, policy)
		}
	}
	preview := classify(http.MethodPost, "/api/v1/operations/ssh/preview")
	if preview.StepUpOperation != "" || preview.BodyClass != BodyAuthTiny || preview.ConcurrencyClass != "ssh_candidate" {
		t.Fatalf("preview policy=%#v", preview)
	}
	read := classify(http.MethodGet, "/api/v1/operations/ssh/posture")
	if read.BodyClass != BodyNone || read.ActionScope != "ssh_management" || read.StepUpOperation != "" {
		t.Fatalf("read policy=%#v", read)
	}
}
