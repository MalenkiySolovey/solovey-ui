package sshmanagement

import (
	"context"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type ProtectionResourceReader interface {
	CurrentPosture(context.Context) (*domain.SSHPostureV1, error)
}

type ProtectionResourceContributor struct {
	Reader ProtectionResourceReader
	Now    func() time.Time
}

func (ProtectionResourceContributor) Owner() string { return domain.ProtectionResourceOwner }

func (c ProtectionResourceContributor) ListProtectableResources(ctx context.Context) ([]hostresources.ProtectableResource, error) {
	if c.Reader == nil {
		return []hostresources.ProtectableResource{}, nil
	}
	posture, err := c.Reader.CurrentPosture(ctx)
	if err != nil {
		if domain.ErrorCode(err) == domain.ReasonProviderUnavailable {
			return []hostresources.ProtectableResource{}, nil
		}
		return nil, err
	}
	if posture == nil {
		return nil, domain.NewError("resource_projection", domain.ReasonMalformedProviderEvidence)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return domain.ProtectableResources(*posture, now().UTC())
}
