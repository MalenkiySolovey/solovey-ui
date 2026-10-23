//go:build linux && shtorm_extended

package registry

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

func resolvedFixtureLifecycle(t *testing.T, owner adapter.Lifecycle) func(adapter.StartStage) error {
	t.Helper()
	scope := adapter.NewScope(t.Context(), log.NewNOPFactory().Logger())
	t.Cleanup(func() { _ = scope.Close() })
	return func(stage adapter.StartStage) error { return scope.Start("resolved", owner, stage) }
}
