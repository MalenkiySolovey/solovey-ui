package hostsurface

import (
	"context"
	"errors"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type SSHListenerProjection struct {
	ValidatedAt time.Time
	Source      string
	Revision    string
	Authorities []domain.SSHListenerAuthorityV1
	ResourceIDs map[string]string
	Resources   []hostresources.ProtectableResource
}

type SSHProjectionSource interface {
	ProjectSSHListeners(context.Context, func() time.Time) (SSHListenerProjection, error)
}

type SSHPostureReader interface {
	CurrentPosture(context.Context) (*domain.SSHPostureV1, error)
}

type SSHPostureProjectionSource struct{ Reader SSHPostureReader }

func (s SSHPostureProjectionSource) ProjectSSHListeners(ctx context.Context, clock func() time.Time) (SSHListenerProjection, error) {
	if s.Reader == nil {
		return SSHListenerProjection{}, errors.New("SSH posture reader is unavailable")
	}
	posture, err := s.Reader.CurrentPosture(ctx)
	now := clock().UTC()
	if err != nil || posture == nil || posture.Validate(now.UTC()) != nil {
		return SSHListenerProjection{}, errors.New("SSH posture projection is unavailable")
	}
	projection := SSHListenerProjection{ValidatedAt: now, Source: "sshbroker:listener-authority", Revision: posture.SemanticRevision,
		Authorities: make([]domain.SSHListenerAuthorityV1, 0, len(posture.ListenerAuthorities)),
		ResourceIDs: make(map[string]string, len(posture.ListenerAuthorities))}
	resources, err := domain.ProtectableResources(*posture, now.UTC())
	if err != nil {
		return SSHListenerProjection{}, errors.New("SSH resource projection is unavailable")
	}
	projection.Resources = resources
	for _, authority := range posture.ListenerAuthorities {
		if !authority.Valid(now.UTC()) {
			continue
		}
		resourceID := domain.ProtectionResourceID(authority)
		if resourceID == "" || projection.ResourceIDs[authority.Revision] != "" {
			return SSHListenerProjection{}, errors.New("SSH listener resource projection is ambiguous")
		}
		projection.Authorities = append(projection.Authorities, authority)
		projection.ResourceIDs[authority.Revision] = resourceID
	}
	if !exactOwnerRevision(projection.Revision) || len(projection.Authorities) == 0 || len(projection.Authorities) > 64 || len(projection.ResourceIDs) != len(projection.Authorities) || len(projection.Resources) != len(projection.Authorities) {
		return SSHListenerProjection{}, errors.New("SSH listener projection is empty or unbounded")
	}
	return projection, nil
}
