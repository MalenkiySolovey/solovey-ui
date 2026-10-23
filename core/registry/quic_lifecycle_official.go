//go:build with_quic && !shtorm_extended

package registry

func (h *hysteriaControlled) Close() error  { return h.close(h.Inbound.Close) }
func (h *hysteria2Controlled) Close() error { return h.close(h.Inbound.Close) }
func (h *tuicControlled) Close() error      { return h.close(h.Inbound.Close) }
