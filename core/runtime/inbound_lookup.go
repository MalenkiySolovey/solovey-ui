package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/sagernet/sing-box/option"
)

type InboundRuntimeRecord struct {
	Tag               string
	Type              string
	OptionsDigest     string
	ManagerGeneration uint64
}

func inboundRuntimeRecord(ctx context.Context, options option.Inbound, generation uint64) (InboundRuntimeRecord, bool) {
	content, err := options.MarshalJSONContext(ctx)
	if err != nil {
		return InboundRuntimeRecord{}, false
	}
	sum := sha256.Sum256(content)
	return InboundRuntimeRecord{
		Tag: options.Tag, Type: options.Type,
		OptionsDigest: hex.EncodeToString(sum[:]), ManagerGeneration: generation,
	}, true
}

func (c *Core) LookupInbound(tag string) (runtimeAvailable bool, inboundType string, inboundTag string, present bool) {
	c.access.RLock()
	defer c.access.RUnlock()
	if !c.isRunning || c.instance == nil || c.inboundManager == nil {
		return false, "", "", false
	}
	inbound, loaded := c.inboundManager.Get(tag)
	if !loaded || inbound == nil {
		return true, "", "", false
	}
	return true, inbound.Type(), inbound.Tag(), true
}

func (c *Core) LookupInboundRuntime(tag string) (runtimeAvailable bool, records []InboundRuntimeRecord) {
	c.access.RLock()
	defer c.access.RUnlock()
	if !c.isRunning || c.instance == nil || c.inboundManager == nil {
		return false, nil
	}
	inbound, loaded := c.inboundManager.Get(tag)
	if !loaded || inbound == nil {
		return true, nil
	}
	record, recorded := c.effectiveInbounds[tag]
	if !recorded || record.Tag != inbound.Tag() || record.Type != inbound.Type() {
		return true, []InboundRuntimeRecord{{Tag: inbound.Tag(), Type: inbound.Type(), ManagerGeneration: c.managerGeneration}}
	}
	return true, []InboundRuntimeRecord{record}
}

// LookupInboundExact is the bounded cross-layer attestation used by neutral
// capability providers. It exposes no options, credentials, or runtime object.
func (c *Core) LookupInboundExact(tag string) (runtimeAvailable bool, inboundType, inboundTag, optionsDigest string, managerGeneration uint64, present bool) {
	available, records := c.LookupInboundRuntime(tag)
	if !available || len(records) != 1 {
		return available, "", "", "", 0, false
	}
	record := records[0]
	return true, record.Type, record.Tag, record.OptionsDigest, record.ManagerGeneration, true
}
