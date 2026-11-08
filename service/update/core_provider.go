package update

import (
	"context"
	"errors"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

var (
	ErrCoreProviderUnverified  = errors.New("installed core provider identity is unverified")
	ErrCoreProviderMaintenance = errors.New("release artifacts lack the identity required for core provider maintenance")
)

// The released v1 artifact contract describes the existing Official set. It
// contains no core-provider identity, so it cannot authorize an Extended
// artifact or a provider transition. In particular, a valid publisher signature
// must not turn an Extended installation into an implicit Official downgrade.
// Rollback/reconcile keep their existing checkpoint authority independently.
func (m *LifecycleManager) legacyCoreProviderAdmission() error {
	identity, err := m.installedCoreProvider()
	if err != nil {
		return err
	}
	if identity.ID != coreprovider.DefaultID() {
		return ErrCoreProviderMaintenance
	}
	return nil
}

func (m *LifecycleManager) installedCoreProvider() (coreprovider.Identity, error) {
	if m == nil || m.coreProvider == nil {
		return coreprovider.Identity{}, ErrCoreProviderUnverified
	}
	identity := m.coreProvider()
	if !coreprovider.SourceIdentityVerified(identity) {
		return coreprovider.Identity{}, ErrCoreProviderUnverified
	}
	return identity, nil
}

// Ordinary updates select the installed provider. The signed target source is
// verified against the candidate executable by Artifact/Trust and Root; it can
// advance within this provider. v1 remains restricted to Official.
func (m *LifecycleManager) ordinaryArtifacts(verified release.Verified, platform, arch, profile string) ([]release.Artifact, error) {
	identity, err := m.installedCoreProvider()
	if err != nil {
		return nil, err
	}
	if verified.Manifest.Schema == release.SchemaV1 && identity.ID != coreprovider.DefaultID() {
		return nil, ErrCoreProviderMaintenance
	}
	artifacts, err := verified.PanelArtifactsForProvider(platform, arch, profile, identity.ID)
	if err != nil || len(artifacts) != 1 {
		return nil, ErrCoreProviderMaintenance
	}
	trusted, err := verified.ArtifactIdentity(artifacts[0])
	if err != nil || verified.Manifest.Schema == release.SchemaV2 && trusted.CoreProvider.ID != identity.ID {
		return nil, ErrCoreProviderMaintenance
	}
	return artifacts, nil
}

// The host adapter owns the durable prepared provider binding. Keeping that
// authority in its existing checkpoint avoids adding provider claims to SQLite
// operation rows or re-fetching mutable channel metadata during activation.
type ordinaryActivationAdmitter interface {
	AdmitOrdinaryActivation(context.Context, model.UpdateOperation, release.CoreProviderSource) error
}

func (m *LifecycleManager) ordinaryActivationAdmission(ctx context.Context, operation model.UpdateOperation) error {
	identity, err := m.installedCoreProvider()
	if err != nil {
		return err
	}
	if owner, ok := m.provider.(ordinaryActivationAdmitter); ok {
		return owner.AdmitOrdinaryActivation(ctx, operation, identity.Source)
	}
	return m.legacyCoreProviderAdmission()
}
