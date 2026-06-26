package runtime

import (
	"errors"
	"fmt"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/protocol/group"
)

var (
	ErrGroupNotFound    = errors.New("failover group not found")
	ErrNotSelectorGroup = errors.New("outbound is not a selector group")
	ErrMemberNotInGroup = errors.New("member not in group")
)

// SelectGroupMember is the sole concrete selector assertion in panel code.
// Callers re-resolve on every cycle so core restarts cannot stale-cache it.
func (c *Core) SelectGroupMember(groupTag, memberTag string) error {
	return c.withRuntime(func(current coreRuntime) error {
		outbound, ok := current.outboundManager.Outbound(groupTag)
		if !ok {
			return fmt.Errorf("%w: %q", ErrGroupNotFound, groupTag)
		}
		selector, ok := outbound.(*group.Selector)
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotSelectorGroup, groupTag)
		}
		if !selector.SelectOutbound(memberTag) {
			return fmt.Errorf("%w: %q in %q", ErrMemberNotInGroup, memberTag, groupTag)
		}
		return nil
	})
}

func (c *Core) GroupNow(groupTag string) (active string, ok bool) {
	_ = c.withRuntime(func(current coreRuntime) error {
		outbound, found := current.outboundManager.Outbound(groupTag)
		if !found {
			return nil
		}
		outboundGroup, isGroup := outbound.(adapter.OutboundGroup)
		if !isGroup {
			return nil
		}
		active = outboundGroup.Now()
		ok = true
		return nil
	})
	return active, ok
}
