package runtime

import (
	"context"
	"sort"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"
	"github.com/sagernet/sing-box/daemon"
)

const maxConnectionFrameEvents = 4096
const maxConnectionSnapshot = 256
const maxDisconnectFlows = 128

type RuntimeConnection struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	ClientID      uint   `json:"clientId,omitempty"`
	Identity      string `json:"identity"`
	Inbound       string `json:"inbound"`
	InboundType   string `json:"inboundType"`
	Outbound      string `json:"outbound"`
	Network       string `json:"network"`
	Protocol      string `json:"protocol"`
	Source        string `json:"source"`
	Destination   string `json:"destination"`
	CreatedAt     int64  `json:"createdAt"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	ParentControl string `json:"parentControl"`
}

type ConnectionSnapshot struct {
	Generation       string              `json:"generation"`
	ObservedAt       int64               `json:"observedAt"`
	Source           string              `json:"source"`
	Connections      []RuntimeConnection `json:"connections"`
	Parents          []RuntimeQUICParent `json:"parents"`
	ParentTotal      int                 `json:"parentTotal"`
	ParentsTruncated bool                `json:"parentsTruncated"`
	Total            int                 `json:"total"`
	ActualTotal      int                 `json:"actualTotal"`
	Unassociated     int                 `json:"unassociated"`
	Limit            int                 `json:"limit"`
	Truncated        bool                `json:"truncated"`
}

func readConnectionFrame(ctx context.Context, api *privateAPI) ([]*daemon.Connection, error) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := api.rpc.SubscribeConnections(readCtx, &daemon.SubscribeConnectionsRequest{Interval: int64(time.Second)})
	if err != nil {
		return nil, ErrRuntimeAPIUnavailable
	}
	frame, err := stream.Recv()
	if err != nil || !frame.Reset_ {
		return nil, ErrRuntimeAPIUnavailable
	}
	if len(frame.Events) > maxConnectionFrameEvents {
		return nil, ErrRuntimeLimit
	}
	// The pinned initial frame includes closed history. A close concurrent with
	// enumeration can put one ID in both sections; the closed witness wins.
	active := map[string]*daemon.Connection{}
	closed := map[string]bool{}
	for _, event := range frame.Events {
		flow := event.Connection
		if flow == nil || flow.Id == "" {
			continue
		}
		if flow.ClosedAt != 0 || event.ClosedAt != 0 || event.Type == daemon.ConnectionEventType_CONNECTION_EVENT_CLOSED {
			closed[flow.Id] = true
		} else if event.Type == daemon.ConnectionEventType_CONNECTION_EVENT_NEW {
			active[flow.Id] = flow
		}
	}
	result := make([]*daemon.Connection, 0, len(active))
	for id, flow := range active {
		if !closed[id] {
			result = append(result, flow)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return result[i].Id < result[j].Id
		}
		return result[i].CreatedAt > result[j].CreatedAt
	})
	return result, nil
}

func (c *Core) connectionView(flow *daemon.Connection) RuntimeConnection {
	result := RuntimeConnection{ID: flow.Id, Kind: "flow", Status: "active", Identity: "unavailable", Inbound: redact.StringLimit(flow.Inbound, 256), InboundType: flow.InboundType,
		Outbound: redact.StringLimit(flow.Outbound, 256), Network: flow.Network, Protocol: flow.Protocol, Source: redact.StringLimit(flow.Source, 256),
		Destination: redact.StringLimit(flow.Destination, 256), CreatedAt: flow.CreatedAt, Upload: flow.UplinkTotal, Download: flow.DownlinkTotal, ParentControl: "not_supported"}
	// Caller owns a lifecycle lease; the only principal catalogue belongs to Box.
	if binding, known := c.instance.InboundIdentity().Resolve(flow.Inbound, flow.User); known {
		result.ClientID, result.Identity = binding.ClientID, "verified"
	}
	return result
}

func (c *Core) Connections(ctx context.Context, generation string, clientID uint, limit int) (ConnectionSnapshot, error) {
	result := ConnectionSnapshot{Connections: []RuntimeConnection{}, Parents: []RuntimeQUICParent{}, Source: "official_api", Limit: limit}
	if generation == "" {
		return result, ErrStaleGeneration
	}
	if limit < 1 || limit > maxConnectionSnapshot {
		return result, ErrRuntimeLimit
	}
	if c.snapshotReaders.Add(1) > 8 {
		c.snapshotReaders.Add(-1)
		return result, ErrRuntimeLimit
	}
	defer c.snapshotReaders.Add(-1)
	parents, parentTotal, parentsTruncated, err := c.quicParentSnapshot(ctx, generation, clientID, limit)
	if err != nil {
		return result, err
	}
	err = c.withPrivateAPI(ctx, generation, func(ctx context.Context, api *privateAPI) error {
		flows, err := readConnectionFrame(ctx, api)
		if err != nil {
			return err
		}
		result.Generation, result.ObservedAt, result.ActualTotal = api.generation, time.Now().UnixMilli(), len(flows)
		for _, flow := range flows {
			view := c.connectionView(flow)
			if view.Identity == "unavailable" {
				result.Unassociated++
			}
			if clientID != 0 && view.ClientID != clientID {
				continue
			}
			result.Total++
			if len(result.Connections) < limit {
				result.Connections = append(result.Connections, view)
			}
		}
		result.Truncated = result.Total > len(result.Connections)
		result.Parents, result.ParentTotal, result.ParentsTruncated = parents, parentTotal, parentsTruncated
		return nil
	})
	return result, err
}

type DisconnectRequest struct {
	Generation string             `json:"generation"`
	FlowID     string             `json:"flowId,omitempty"`
	ClientID   uint               `json:"clientId,omitempty"`
	Parents    []QUICParentTarget `json:"parents,omitempty"`
}

type DisconnectResult struct {
	Generation       string `json:"generation"`
	Outcome          string `json:"outcome"`
	Matched          int    `json:"matched"`
	Closed           int    `json:"closed"`
	Remaining        int    `json:"remaining"`
	ParentClosed     bool   `json:"parentClosed"`
	ParentControl    string `json:"parentControl"`
	Reason           string `json:"reason,omitempty"`
	ParentsMatched   int    `json:"parentsMatched"`
	ParentsClosed    int    `json:"parentsClosed"`
	ParentsRemaining int    `json:"parentsRemaining"`
}

// Disconnect closes only reverified official IDs. Upstream RPC acknowledgement
// ignores the underlying close error, so disappearance is checked before a
// success claim. Explicit QUIC targets use the authenticated transport owner;
// flow-only requests retain the original partial parent-control semantics.
func (c *Core) Disconnect(ctx context.Context, request DisconnectRequest) (DisconnectResult, error) {
	result := DisconnectResult{Generation: request.Generation, Outcome: "ERROR", ParentControl: "not_supported"}
	if request.Generation == "" {
		return result, ErrStaleGeneration
	}
	if request.FlowID == "" && request.ClientID == 0 {
		return result, ErrRuntimeLimit
	}
	c.mutation.Lock()
	defer c.mutation.Unlock()
	err := c.withPrivateAPI(ctx, request.Generation, func(ctx context.Context, api *privateAPI) error {
		if len(request.Parents) != 0 {
			return c.disconnectQUICParents(ctx, request, &result)
		}
		flows, err := readConnectionFrame(ctx, api)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, flow := range flows {
			if request.FlowID != "" && request.FlowID != flow.Id {
				continue
			}
			if request.ClientID != 0 && c.connectionView(flow).ClientID != request.ClientID {
				continue
			}
			ids = append(ids, flow.Id)
		}
		result.Matched = len(ids)
		if len(ids) == 0 {
			result.Outcome = "ALREADY_GONE"
			return nil
		}
		if len(ids) > maxDisconnectFlows {
			result.Outcome = "NOT_SUPPORTED"
			result.Reason = "runtime_limit_exceeded"
			return ErrRuntimeLimit
		}
		for _, id := range ids {
			if _, err := api.rpc.CloseConnection(ctx, &daemon.CloseConnectionRequest{Id: id}); err != nil {
				result.Outcome = "PARTIAL_DISCONNECT"
				return ErrRuntimeAPIUnavailable
			}
		}
		remaining, err := readConnectionFrame(ctx, api)
		if err != nil {
			result.Outcome = "PARTIAL_DISCONNECT"
			return err
		}
		alive := map[string]bool{}
		for _, flow := range remaining {
			alive[flow.Id] = true
		}
		for _, id := range ids {
			if alive[id] {
				result.Remaining++
			} else {
				result.Closed++
			}
		}
		if result.Remaining != 0 || request.FlowID == "" {
			result.Outcome = "PARTIAL_DISCONNECT"
			if result.Remaining == 0 {
				result.Reason = "observed_flows_closed_parent_control_unavailable"
			} else {
				result.Reason = "flows_remain"
			}
		} else {
			result.Outcome = "FLOW_CLOSED"
		}
		return nil
	})
	return result, err
}
