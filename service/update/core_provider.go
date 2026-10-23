package update

import (
	"errors"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
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
	if m == nil || m.coreProvider == nil {
		return ErrCoreProviderUnverified
	}
	identity := m.coreProvider()
	if !coreprovider.SourceIdentityVerified(identity) {
		return ErrCoreProviderUnverified
	}
	if identity.ID != coreprovider.DefaultID() {
		return ErrCoreProviderMaintenance
	}
	return nil
}
