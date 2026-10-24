package update

import (
	"context"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"gorm.io/gorm"
)

const ProviderTransitionSchema = "solovey.provider-maintenance-preview/v1"

type ProviderTransitionRequest struct {
	Channel                       release.Channel    `json:"channel"`
	TargetProvider                string             `json:"targetProvider"`
	ExpectedSequence              uint64             `json:"expectedSequence"`
	ExpectedManifestDigest        string             `json:"expectedManifestDigest"`
	ExpectedArtifactSetDigest     string             `json:"expectedArtifactSetDigest"`
	ExpectedConfigurationRevision string             `json:"expectedConfigurationRevision"`
	ExpectedProviderRevision      string             `json:"expectedProviderRevision"`
	IdempotencyKey                string             `json:"idempotencyKey"`
	Acknowledged                  bool               `json:"acknowledged"`
	Authorize                     actions.Authorizer `json:"-"`
}

func (request ProviderTransitionRequest) Validate() error {
	if !request.Acknowledged || !knownProvider(request.TargetProvider) || !safeID(request.IdempotencyKey, 96) ||
		(request.Channel != release.ChannelMain && request.Channel != release.ChannelBeta) || request.ExpectedSequence == 0 ||
		!validDigest(request.ExpectedManifestDigest) || !validDigest(request.ExpectedArtifactSetDigest) ||
		!validDigest(request.ExpectedConfigurationRevision) || !validDigest(request.ExpectedProviderRevision) {
		return ErrCoreProviderMaintenance
	}
	return nil
}

func ProviderTransitionAction(request ProviderTransitionRequest) actions.Invocation {
	reference := operationcoordination.NewReference("update", "panel", updateOperationID(request.IdempotencyKey, request.ExpectedManifestDigest))
	return actions.MaintainProvider(reference, actions.ProviderMaintenanceInput{PlanDigest: semanticDigest(request)})
}

func ProviderTransitionConfirmation(request ProviderTransitionRequest) string {
	digest := semanticDigest(request)
	return "MAINTAIN_CORE_PROVIDER_" + strings.ToUpper(digest[:12])
}

type ProviderTransitionPreview struct {
	Schema            string                     `json:"schema"`
	State             string                     `json:"state"`
	Channel           release.Channel            `json:"channel"`
	Sequence          uint64                     `json:"sequence"`
	ManifestDigest    string                     `json:"manifestDigest"`
	ArtifactSetDigest string                     `json:"artifactSetDigest"`
	Current           coreprovider.Identity      `json:"current"`
	Target            release.CoreProviderSource `json:"target"`
	ArtifactTrust     release.ArtifactIdentity   `json:"artifactTrust"`
	Configuration     CoreConfigurationSnapshot  `json:"configuration"`
	ReasonCodes       []string                   `json:"reasonCodes"`
}

// Composition supplies the same ConfigService owner used for save/apply and
// deliberate maintenance. This does not select a provider or authorize a flip.
func (m *LifecycleManager) SetCoreConfigurationOwner(owner CoreConfigurationOwner) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configuration = owner
	if provider, ok := m.provider.(interface{ setCoreConfigurationOwner(CoreConfigurationOwner) }); ok {
		provider.setCoreConfigurationOwner(owner)
	}
}

// PreviewProviderTransition is read-only. A publisher signature binds the
// selected artifact but cannot prove that the candidate accepts this config;
// staged target validation remains a mandatory maintenance gate.
func (m *LifecycleManager) PreviewProviderTransition(ctx context.Context, channel release.Channel, target string) (ProviderTransitionPreview, error) {
	result := ProviderTransitionPreview{Schema: ProviderTransitionSchema, State: "UNAVAILABLE", Channel: channel, ReasonCodes: []string{}}
	if m == nil || !knownProvider(target) {
		return result, ErrCoreProviderMaintenance
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configuration == nil || m.coreProvider == nil {
		return result, ErrProviderUnavailable
	}
	current := m.coreProvider()
	if !coreprovider.SourceIdentityVerified(current) {
		return result, ErrCoreProviderUnverified
	}
	lastApplied, err := m.repo.lastApplied(ctx, channel)
	if err != nil {
		return result, err
	}
	verified, artifacts, err := m.fetchProviderRelease(ctx, channel, target, lastApplied)
	if err != nil {
		return result, err
	}
	identity, err := verified.ArtifactIdentity(artifacts[0])
	if err != nil {
		return result, err
	}
	configuration, err := m.configuration.CaptureProviderConfiguration(ctx)
	if err != nil || !validDigest(configuration.ConfigurationDigest) || !validDigest(configuration.ConfigurationRevision) || !validDigest(configuration.CapabilityRevision) {
		configuration.Clear()
		return result, ErrCoreProviderMaintenance
	}
	configuration.Clear()
	result.State, result.Current, result.Target = "TARGET_VALIDATION_REQUIRED", current, identity.CoreProvider
	result.Sequence, result.ManifestDigest, result.ArtifactSetDigest = verified.Manifest.Sequence, verified.Digest, release.ArtifactSetDigest(artifacts)
	result.ArtifactTrust, result.Configuration = identity, configuration
	result.ReasonCodes = []string{"provider_maintenance_required", "staged_target_validation_required"}
	return result, nil
}

func knownProvider(value string) bool { return value == "sing-box" || value == "shtorm-extended" }

func (m *LifecycleManager) fetchProviderRelease(ctx context.Context, channel release.Channel, target string, lastApplied uint64) (release.Verified, []release.Artifact, error) {
	if m.coreProvider == nil || !coreprovider.SourceIdentityVerified(m.coreProvider()) {
		return release.Verified{}, nil, ErrCoreProviderUnverified
	}
	if !knownProvider(target) {
		return release.Verified{}, nil, ErrCoreProviderMaintenance
	}
	source, ok := m.sources[channel]
	if !ok {
		return release.Verified{}, nil, ErrCoreProviderMaintenance
	}
	raw, err := m.fetcher.Fetch(ctx, source)
	if err != nil {
		return release.Verified{}, nil, err
	}
	// Signature, expiry, root/channel limits and canonical membership are still
	// verified. Equal sequence is usable only for the exact already-applied
	// envelope and a different installed provider; root also enforces its global
	// activation high-water mark. Ordinary release verification stays strict.
	verified, err := release.Verify(raw, m.trust, m.now(), channel, 0)
	if err != nil || verified.Manifest.Schema != release.SchemaV2 || verified.Manifest.Sequence < lastApplied {
		return release.Verified{}, nil, ErrCoreProviderMaintenance
	}
	if verified.Manifest.Sequence == lastApplied {
		installed, err := m.repo.appliedManifest(ctx, channel, lastApplied)
		if err != nil || installed != verified.Digest || target == m.coreProvider().ID {
			return release.Verified{}, nil, ErrReleaseChanged
		}
	}
	if err := validateManifestCompatibility(verified); err != nil {
		return release.Verified{}, nil, err
	}
	for _, artifact := range verified.Manifest.Artifacts {
		if artifact.Provenance != source.ExpectedProvenance {
			return release.Verified{}, nil, errors.New("release artifact provenance does not match the pinned source policy")
		}
	}
	platform, arch := m.platform()
	selected, err := verified.PanelArtifactsForProvider(platform, arch, m.profile(), target)
	if err != nil || len(selected) != 1 {
		return release.Verified{}, nil, ErrCoreProviderMaintenance
	}
	return verified, selected, nil
}

// LastObserved/LastVerified may describe a newer checked envelope. Equal-sequence
// maintenance instead needs the exact previously applied envelope, retained by
// the existing Update operation history. Missing/pruned authority fails closed;
// the root broker independently enforces its global manifest high-water mark.
func (r Repository) appliedManifest(ctx context.Context, channel release.Channel, sequence uint64) (string, error) {
	db, err := r.db(ctx)
	if err != nil {
		return "", err
	}
	var digest string
	err = db.Transaction(func(tx *gorm.DB) error {
		var state model.UpdateReleaseState
		if err := tx.First(&state, "channel = ?", channel).Error; err != nil {
			return err
		}
		if !validPersistedReleaseState(state) || state.LastAppliedSequence != sequence || restoredReleaseState(state) {
			return ErrRecoveryRequired
		}
		var operations []model.UpdateOperation
		if err := tx.Where("channel = ? AND sequence = ? AND state = ?", channel, sequence, StateApplied).
			Limit(updateTerminalHistoryCount + 1).Find(&operations).Error; err != nil {
			return err
		}
		if len(operations) == 0 || len(operations) > updateTerminalHistoryCount {
			return ErrRecoveryRequired
		}
		for _, operation := range operations {
			if !validPersistedOperation(operation) || operation.RestoredUntrusted || digest != "" && digest != operation.ManifestDigest {
				return ErrRecoveryRequired
			}
			digest = operation.ManifestDigest
		}
		return nil
	})
	return digest, err
}
