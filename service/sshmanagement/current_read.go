package sshmanagement

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type currentPostureKey struct{}

type currentPostureRead struct {
	manager *Manager
	once    sync.Once
	posture *domain.SSHPostureV1
	err     error
}

// WithCurrentPosture bounds a composed refresh to one SSH provider observation.
// The lazy observation starts at the first consumer, has no global lifetime,
// and cannot silently refresh to a different generation if it expires.
func (m *Manager) WithCurrentPosture(ctx context.Context) context.Context {
	return context.WithValue(ctx, currentPostureKey{}, &currentPostureRead{manager: m})
}

func (r *currentPostureRead) read(ctx context.Context) (*domain.SSHPostureV1, time.Time, error) {
	r.once.Do(func() { r.posture, _, r.err = r.manager.observeCurrentPosture(ctx) })
	now := r.manager.now()
	if r.err != nil {
		return nil, now, r.err
	}
	if err := r.posture.Validate(now); err != nil {
		return nil, now, err
	}
	// Each consumer receives its own bounded value; projections cannot change
	// another consumer's authority through shared slices or pointers.
	payload, err := json.Marshal(r.posture)
	if err != nil {
		return nil, now, err
	}
	var posture domain.SSHPostureV1
	if err := json.Unmarshal(payload, &posture); err != nil {
		return nil, now, err
	}
	return &posture, now, nil
}

// CurrentReadV1 composes one read-only SSH observation with neutral owner
// facts. It never populates the persisted policy-workflow history.
type CurrentReadV1 struct {
	State        string                               `json:"state"`
	Fresh        bool                                 `json:"fresh"`
	Posture      *domain.SSHPostureV1                 `json:"posture"`
	Endpoints    []hostresources.ManagementEndpointV1 `json:"endpoints"`
	Capabilities domain.CapabilitySetV1               `json:"capabilities"`
	Recovery     managementregistry.EvidenceSnapshot  `json:"recovery"`
	ReasonCodes  []domain.ReasonCode                  `json:"reasonCodes"`
	ValidatedAt  time.Time                            `json:"validatedAt"`
}

func (m *Manager) CurrentRead(ctx context.Context) CurrentReadV1 {
	value := CurrentReadV1{State: "UNAVAILABLE", Capabilities: m.capabilities(ctx), ReasonCodes: []domain.ReasonCode{}}
	// Collect other owners before observing the short-lived SSH authority.
	// A slow resource/evidence provider cannot age that authority after its
	// final validation while the HTTP envelope still claims it is fresh.
	endpoints := m.endpoints(ctx, m.now())
	value.Recovery = m.evidence(ctx, m.now())
	posture, validatedAt, err := m.currentPosture(ctx)
	value.ValidatedAt = validatedAt
	value.Endpoints = make([]hostresources.ManagementEndpointV1, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if hostresources.ManagementEndpointCurrent(endpoint, validatedAt) {
			value.Endpoints = append(value.Endpoints, endpoint)
		}
	}
	if err != nil {
		value.ReasonCodes = append(value.ReasonCodes, domain.ErrorCode(err))
	} else {
		value.State, value.Fresh, value.Posture = "OBSERVED", true, posture
		endpoints, _ := domain.CurrentManagementEndpoints(*posture, value.ValidatedAt)
		value.Endpoints = mergeEndpoints(value.Endpoints, endpoints)
	}
	value.Recovery.Paths = effectiveEvidence(value.Recovery.Paths, value.Endpoints, value.ValidatedAt)
	return value
}

func neutralEndpoints(ctx context.Context, now time.Time) []hostresources.ManagementEndpointV1 {
	resources := hostresources.SnapshotExcluding(ctx, domain.ProtectionResourceOwner)
	return managementregistry.Endpoints(resources.Resources, hostfacts.Snapshot{}, now)
}
