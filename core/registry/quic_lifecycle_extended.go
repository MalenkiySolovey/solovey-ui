//go:build with_quic && shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter"

func (q *quicInboundControl) startNative(stage adapter.StartStage, scope *adapter.Scope, start func(adapter.StartStage, *adapter.Scope) error) error {
	if stage == adapter.StartStateInitialize {
		q.closeNative = scope.Close
	}
	err := start(stage, scope)
	if stage == adapter.StartStateStart {
		// Registered after native startup, including its failure path. Reverse
		// cleanup drains parents before the native UDP listener is destroyed.
		scope.Add(q.owner.Close)
	}
	return err
}

func (q *quicInboundControl) closeNativeScope() error {
	return q.close(func() error {
		if q.closeNative != nil {
			return q.closeNative()
		}
		return nil
	})
}

func (h *hysteriaControlled) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return h.startNative(stage, scope, h.Inbound.Start)
}
func (h *hysteria2Controlled) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return h.startNative(stage, scope, h.Inbound.Start)
}
func (h *tuicControlled) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return h.startNative(stage, scope, h.Inbound.Start)
}
func (h *hysteriaControlled) Close() error  { return h.closeNativeScope() }
func (h *hysteria2Controlled) Close() error { return h.closeNativeScope() }
func (h *tuicControlled) Close() error      { return h.closeNativeScope() }
