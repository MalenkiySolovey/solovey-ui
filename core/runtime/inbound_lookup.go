package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

type InboundRuntimeRecord struct {
	Tag               string
	Type              string
	OptionsDigest     string
	ManagerGeneration uint64
	ownerTag          string
}

func inboundRuntimeRecords(ctx context.Context, options option.Inbound, generation uint64) ([]InboundRuntimeRecord, bool) {
	root, ok := inboundRuntimeRecord(ctx, options, generation)
	if !ok {
		return nil, false
	}
	members, err := registry.InboundMembers(options.Type, options.Options)
	if err != nil {
		return nil, false
	}
	result := []InboundRuntimeRecord{root}
	for _, member := range members {
		record, ok := inboundRuntimeRecord(ctx, member, generation)
		if !ok {
			return nil, false
		}
		record.ownerTag = options.Tag
		result = append(result, record)
	}
	return result, true
}

// Group members remain owned by their native serving Scope. Inspect the
// actual objects without registering duplicate top-level runtime inbounds.
func runtimeInbounds(manager adapter.InboundManager) []adapter.Inbound {
	queue := append([]adapter.Inbound(nil), manager.Inbounds()...)
	result := make([]adapter.Inbound, 0, len(queue))
	seen := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil || seen[current.Tag()] {
			continue
		}
		seen[current.Tag()] = true
		result = append(result, current)
		if group, ok := current.(interface{ Inbounds() []adapter.Inbound }); ok {
			queue = append(queue, group.Inbounds()...)
		}
	}
	return result
}

func findRuntimeInbound(manager adapter.InboundManager, tag string) (adapter.Inbound, bool) {
	if inbound, ok := manager.Get(tag); ok {
		return inbound, true
	}
	for _, inbound := range runtimeInbounds(manager) {
		if inbound.Tag() == tag {
			return inbound, true
		}
	}
	return nil, false
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
	inbound, loaded := findRuntimeInbound(c.inboundManager, tag)
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
	inbound, loaded := findRuntimeInbound(c.inboundManager, tag)
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
