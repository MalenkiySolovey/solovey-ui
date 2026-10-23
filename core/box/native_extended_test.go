//go:build shtorm_extended

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

func fixtureLifecycle(owner adapter.Lifecycle) (func(adapter.StartStage) error, func() error) {
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().Logger())
	return func(stage adapter.StartStage) error { return scope.Start("fixture", owner, stage) }, scope.Close
}
func (*dnsCacheWitness) Start(adapter.StartStage, *adapter.Scope) error { return nil }
func (*dnsRuleWitness) Start(adapter.StartStage, *adapter.Scope) error  { return nil }
func (p *foundationProvider) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(p.Close)
	}
	return p.start(stage)
}
func (i *foundationInbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(i.Close)
	}
	return i.start(stage)
}
