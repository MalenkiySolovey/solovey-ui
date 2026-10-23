//go:build linux && !shtorm_extended

package registry

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
)

func resolvedFixtureLifecycle(t *testing.T, owner adapter.Lifecycle) func(adapter.StartStage) error {
	t.Helper()
	t.Cleanup(func() { _ = owner.Close() })
	return owner.Start
}
