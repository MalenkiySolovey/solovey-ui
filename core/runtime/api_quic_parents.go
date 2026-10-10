package runtime

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
)

var ErrStaleInboundEpoch = errors.New("stale inbound epoch")
var ErrQUICParentIdentity = errors.New("QUIC parent identity unavailable or scope mismatch")

type QUICParentTarget struct {
	ParentID string `json:"parentId"`
	Inbound  string `json:"inbound"`
	Epoch    string `json:"epoch"`
}

type RuntimeQUICParent struct {
	QUICParentTarget
	ClientID      uint   `json:"clientId"`
	InboundType   string `json:"inboundType"`
	CreatedAt     int64  `json:"createdAt"`
	ParentControl string `json:"parentControl"`
}

func (c *Core) quicParentSnapshot(ctx context.Context, generation string, clientID uint, limit int) ([]RuntimeQUICParent, int, bool, error) {
	parents, total, truncated := []RuntimeQUICParent{}, 0, false
	if !registry.QUICParentControlCompiled() {
		return parents, total, truncated, nil
	}
	// Keep the established mutation -> lifecycle lock order. Reads fail boundedly
	// during a hot mutation instead of holding that lease across a streaming RPC.
	if !c.mutation.TryRLock() {
		return parents, total, truncated, ErrRuntimeLimit
	}
	defer c.mutation.RUnlock()
	err := c.withPrivateAPI(ctx, generation, func(_ context.Context, _ *privateAPI) error {
		parents, total, truncated = c.quicParents(clientID, limit)
		return nil
	})
	return parents, total, truncated, err
}

// This is a bounded projection of per-inbound transport owners, not a second
// live-flow or authentication registry. The caller holds both runtime leases.
func (c *Core) quicParents(clientID uint, limit int) ([]RuntimeQUICParent, int, bool) {
	result := []RuntimeQUICParent{}
	total, examined := 0, 0
	truncated := false
	if c.instance == nil {
		return result, 0, false
	}
	for _, inbound := range c.instance.Inbound().Inbounds() {
		control, ok := inbound.(registry.QUICParentControl)
		if !ok {
			continue
		}
		parents, count := control.QUICParents(maxConnectionFrameEvents - examined)
		examined += len(parents)
		if count > len(parents) {
			truncated = true
		}
		for _, parent := range parents {
			binding, epoch, known := c.instance.InboundIdentity().ResolvePrincipal(inbound.Tag(), parent.Principal)
			if !known || clientID != 0 && binding.ClientID != clientID {
				continue
			}
			total++
			if len(result) < limit {
				result = append(result, RuntimeQUICParent{QUICParentTarget: QUICParentTarget{ParentID: parent.ID, Inbound: inbound.Tag(), Epoch: epoch},
					ClientID: binding.ClientID, InboundType: inbound.Type(), CreatedAt: parent.CreatedAt, ParentControl: "authenticated_quic"})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Inbound != result[j].Inbound {
			return result[i].Inbound < result[j].Inbound
		}
		return result[i].ParentID < result[j].ParentID
	})
	return result, total, truncated || total > len(result)
}

func (c *Core) disconnectQUICParents(ctx context.Context, request DisconnectRequest, result *DisconnectResult) error {
	if request.ClientID == 0 || request.FlowID != "" || len(request.Parents) > maxDisconnectFlows || c.instance == nil {
		return ErrRuntimeLimit
	}
	type selectedParent struct {
		control       registry.QUICParentControl
		id, principal string
	}
	selected := []selectedParent{}
	seen := map[QUICParentTarget]bool{}
	// Validate the entire batch before any close, preventing a mixed foreign or
	// stale request from partially disconnecting legitimate current parents.
	for _, target := range request.Parents {
		id, err := strconv.ParseUint(target.ParentID, 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != target.ParentID || target.Inbound == "" || len(target.Inbound) > 256 || seen[target] {
			return ErrRuntimeLimit
		}
		seen[target] = true
		epoch, current := c.instance.InboundIdentity().CurrentEpoch(target.Inbound)
		if !current || epoch != target.Epoch {
			return ErrStaleInboundEpoch
		}
		inbound, found := c.instance.Inbound().Get(target.Inbound)
		if !found {
			return ErrStaleInboundEpoch
		}
		control, supported := inbound.(registry.QUICParentControl)
		if !supported {
			return ErrQUICParentIdentity
		}
		parent, live := control.QUICParent(target.ParentID)
		if !live {
			continue
		} // gone target cannot mutate any replacement handle
		binding, boundEpoch, known := c.instance.InboundIdentity().ResolvePrincipal(target.Inbound, parent.Principal)
		if !known || binding.ClientID != request.ClientID || boundEpoch != target.Epoch {
			return ErrQUICParentIdentity
		}
		selected = append(selected, selectedParent{control: control, id: parent.ID, principal: parent.Principal})
	}
	result.ParentControl, result.ParentsMatched = "authenticated_quic", len(selected)
	if len(selected) == 0 {
		result.Outcome = "ALREADY_GONE"
		return nil
	}
	var closeErr error
	for _, parent := range selected {
		if err := ctx.Err(); err != nil {
			closeErr = errors.Join(closeErr, err)
			break
		}
		if err := parent.control.CloseQUICParent(ctx, parent.id, parent.principal); err != nil {
			closeErr = errors.Join(closeErr, err)
		} else {
			result.ParentsClosed++
		}
	}
	result.ParentsRemaining = result.ParentsMatched - result.ParentsClosed
	result.ParentClosed = result.ParentsClosed > 0
	if closeErr != nil || result.ParentsRemaining != 0 {
		result.Outcome, result.Reason = "PARTIAL_DISCONNECT", "quic_parent_close_incomplete"
		return ErrRuntimeAPIUnavailable
	}
	result.Outcome, result.Reason = "QUIC_PARENTS_CLOSED", "observed_quic_parents_closed"
	return nil
}
