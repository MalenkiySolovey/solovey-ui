//go:build shtorm_extended

package runtime

import "github.com/sagernet/sing-box/adapter"

func (i *failingHotInbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(i.Close)
	}
	return i.start(stage)
}
