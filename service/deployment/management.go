package deployment

import (
	"context"
	"errors"
	"sort"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	sshmanagementdomain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	sshmanagement "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	"gorm.io/gorm"
)

// productionManagementPreservation binds native deployment authority to a
// fresh, independently proven SSH recovery path. A panel session is not an
// independent path for an operation that replaces the panel service itself.
func productionManagementPreservation(ctx context.Context, now time.Time) ManagementPreservation {
	manager := sshmanagement.Shared()
	reasons := make([]string, 0, 8)
	capabilities := manager.Capabilities(ctx)
	if capabilities.ObservePosture != sshmanagementdomain.AvailabilityAvailable {
		reasons = append(reasons, "ssh_management_provider_unavailable")
	}
	posture, postureErr := manager.LatestPosture(ctx)
	postureRevision := ""
	currentSSH := false
	if postureErr != nil || posture == nil || posture.Validate(now) != nil {
		reasons = append(reasons, "ssh_management_posture_unavailable")
	} else {
		postureRevision = posture.SemanticRevision
		for _, endpoint := range posture.Endpoints {
			if endpoint.ServiceKind == hostresources.ManagementSSH && endpoint.ObservedListener && hostresources.ManagementEndpointCurrent(endpoint) {
				currentSSH = true
				break
			}
		}
		if !currentSSH {
			reasons = append(reasons, "ssh_management_endpoint_unavailable")
		}
	}
	if _, activeErr := manager.Repository.ActiveCandidate(ctx); activeErr == nil {
		reasons = append(reasons, "ssh_management_candidate_active")
	} else if !errors.Is(activeErr, gorm.ErrRecordNotFound) {
		reasons = append(reasons, "ssh_management_state_unavailable")
	}
	evidence := manager.RecoverySnapshot(ctx)
	if len(evidence.ReasonCodes) != 0 {
		reasons = append(reasons, "ssh_recovery_evidence_ambiguous")
	}
	freshRevision := ""
	for _, path := range evidence.Paths {
		if path.Kind == string(hostresources.ManagementSSH) && hostresources.RecoveryPathFresh(path, now) {
			freshRevision = path.SourceRevision
			break
		}
	}
	if freshRevision == "" {
		reasons = append(reasons, "fresh_independent_ssh_recovery_missing")
	}
	reasons = unique(reasons)
	sort.Strings(reasons)
	result := ManagementPreservation{Ready: len(reasons) == 0, Reasons: reasons}
	result.EvidenceRevision = domain.Revision(struct {
		Ready      bool
		Capability string
		Posture    string
		Recovery   string
		Paths      string
		Reasons    []string
	}{result.Ready, capabilities.Revision, postureRevision, freshRevision, domain.Revision(evidence.Paths), reasons})
	copy := result
	copy.Revision = ""
	result.Revision = domain.Revision(copy)
	return result
}
