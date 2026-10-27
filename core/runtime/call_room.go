package runtime

import (
	"context"
	"errors"
	"strings"
)

var ErrCallRoomUnavailable = errors.New("call room unavailable")

// UseCallRoom borrows a native creator's private room for one trusted owner
// operation. It adds no ordinary status/config projection or credential store.
func (c *Core) UseCallRoom(ctx context.Context, generation, tag, expectedDigest string, use func([]byte) error) error {
	if ctx == nil || ctx.Err() != nil || generation == "" || len(expectedDigest) != 64 || use == nil {
		return ErrCallRoomUnavailable
	}
	c.mutation.RLock()
	defer c.mutation.RUnlock()
	return c.withRuntime(func(rt coreRuntime) error {
		if ctx.Err() != nil || generation != rt.generation || rt.inboundManager == nil {
			return ErrCallRoomUnavailable
		}
		c.access.RLock()
		record, recorded := c.effectiveInbounds[tag]
		c.access.RUnlock()
		if !recorded || record.Type != "call" || record.Tag != tag || record.ManagerGeneration == 0 || record.OptionsDigest != expectedDigest {
			return ErrCallRoomUnavailable
		}
		inbound, present := findRuntimeInbound(rt.inboundManager, tag)
		if !present || inbound == nil || inbound.Type() != "call" || inbound.Tag() != tag {
			return ErrCallRoomUnavailable
		}
		owner, ok := inbound.(interface{ CallJoinLink() (string, bool) })
		if !ok {
			return ErrCallRoomUnavailable
		}
		link, ready := owner.CallJoinLink()
		if !ready || len(link) == 0 || len(link) > 4096 || strings.TrimSpace(link) != link || strings.ContainsAny(link, "\r\n\x00") || ctx.Err() != nil {
			return ErrCallRoomUnavailable
		}
		borrowed := []byte(link)
		defer clear(borrowed)
		if err := use(borrowed); err != nil {
			return ErrCallRoomUnavailable
		}
		return nil
	})
}
