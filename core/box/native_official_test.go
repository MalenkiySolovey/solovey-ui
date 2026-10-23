//go:build !shtorm_extended

package box

import "github.com/sagernet/sing-box/adapter"

func fixtureLifecycle(owner adapter.Lifecycle) (func(adapter.StartStage) error, func() error) {
	return owner.Start, owner.Close
}
func (*dnsCacheWitness) Start(adapter.StartStage) error            { return nil }
func (*dnsRuleWitness) Start(adapter.StartStage) error             { return nil }
func (p *foundationProvider) Start(stage adapter.StartStage) error { return p.start(stage) }
func (i *foundationInbound) Start(stage adapter.StartStage) error  { return i.start(stage) }
