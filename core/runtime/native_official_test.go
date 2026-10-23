//go:build !shtorm_extended

package runtime

import "github.com/sagernet/sing-box/adapter"

func (i *failingHotInbound) Start(stage adapter.StartStage) error { return i.start(stage) }
