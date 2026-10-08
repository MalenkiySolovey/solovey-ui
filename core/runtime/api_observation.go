package runtime

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"
	"github.com/sagernet/sing-box/daemon"
	"google.golang.org/protobuf/types/known/emptypb"
)

type RuntimeStatus struct {
	Generation   string `json:"generation"`
	State        string `json:"state"`
	APIAvailable bool   `json:"apiAvailable"`
	Source       string `json:"source"`
	ObservedAt   int64  `json:"observedAt"`
	StartedAt    int64  `json:"startedAt,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// RuntimeStatus preserves the distinction between core lifecycle and API
// availability. An RPC failure never stops or restarts an otherwise running box.
func (c *Core) RuntimeStatus(ctx context.Context) RuntimeStatus {
	result := RuntimeStatus{State: "unavailable", Source: "core_lifecycle", ObservedAt: time.Now().UnixMilli()}
	if c == nil {
		return result
	}
	c.access.RLock()
	result.State = c.lifecycleState
	running := c.isRunning
	c.access.RUnlock()
	if !running {
		return result
	}
	err := c.withPrivateAPI(ctx, "", func(ctx context.Context, api *privateAPI) error {
		result.Generation = api.generation
		result.State = "running"
		started, err := api.rpc.GetStartedAt(ctx, &emptypb.Empty{})
		if err != nil {
			return ErrRuntimeAPIUnavailable
		}
		result.StartedAt = started.StartedAt
		result.APIAvailable = true
		result.Source = "official_api"
		return nil
	})
	if err != nil {
		result.Reason = "runtime_api_unavailable"
	}
	return result
}

// The lease fences the entire bounded request, including late gRPC responses.
// A caller-supplied generation is mandatory for mutations and derived reads.
func (c *Core) withPrivateAPI(ctx context.Context, generation string, fn func(context.Context, *privateAPI) error) error {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	c.access.RLock()
	api, running := c.privateAPI, c.isRunning
	c.access.RUnlock()
	if !running || api == nil {
		return ErrCoreUnavailable
	}
	if generation != "" && generation != api.generation {
		return ErrStaleGeneration
	}
	if api.rpc == nil {
		return ErrRuntimeAPIUnavailable
	}
	requestCtx, cancel := context.WithTimeout(ctx, runtimeAPITimeout)
	defer cancel()
	return fn(requestCtx, api)
}

type GroupItem struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	TestedAt int64  `json:"testedAt"`
	Delay    int32  `json:"delay"`
}
type RuntimeGroup struct {
	Tag        string      `json:"tag"`
	Type       string      `json:"type"`
	Selectable bool        `json:"selectable"`
	Selected   string      `json:"selected"`
	Items      []GroupItem `json:"items"`
}
type GroupSnapshot struct {
	Generation string         `json:"generation"`
	ObservedAt int64          `json:"observedAt"`
	Groups     []RuntimeGroup `json:"groups"`
}

func (c *Core) Groups(ctx context.Context, generation string) (GroupSnapshot, error) {
	result := GroupSnapshot{Groups: []RuntimeGroup{}}
	if generation == "" {
		return result, ErrStaleGeneration
	}
	err := c.withPrivateAPI(ctx, generation, func(ctx context.Context, api *privateAPI) error {
		stream, err := api.rpc.SubscribeGroups(ctx, &emptypb.Empty{})
		if err != nil {
			return ErrRuntimeAPIUnavailable
		}
		snapshot, err := stream.Recv()
		if err != nil {
			return ErrRuntimeAPIUnavailable
		}
		if len(snapshot.Group) > 128 {
			return ErrRuntimeLimit
		}
		for _, group := range snapshot.Group {
			if len(group.Items) > 256 {
				return ErrRuntimeLimit
			}
			view := RuntimeGroup{Tag: group.Tag, Type: group.Type, Selectable: group.Selectable, Selected: group.Selected, Items: []GroupItem{}}
			for _, item := range group.Items {
				view.Items = append(view.Items, GroupItem{Tag: item.Tag, Type: item.Type, TestedAt: item.UrlTestTime, Delay: item.UrlTestDelay})
			}
			result.Groups = append(result.Groups, view)
		}
		result.Generation, result.ObservedAt = api.generation, time.Now().UnixMilli()
		return nil
	})
	return result, err
}

// Runtime selection never edits Solovey's desired config. The upstream selector
// may remember its own runtime cache choice; DB/config ownership stays separate.
func (c *Core) SelectRuntimeGroup(ctx context.Context, generation, group, member string) error {
	if generation == "" {
		return ErrStaleGeneration
	}
	c.mutation.Lock()
	defer c.mutation.Unlock()
	return c.withPrivateAPI(ctx, generation, func(ctx context.Context, api *privateAPI) error {
		_, err := api.rpc.SelectOutbound(ctx, &daemon.SelectOutboundRequest{GroupTag: group, OutboundTag: member})
		if err != nil {
			return ErrRuntimeAPIUnavailable
		}
		return nil
	})
}

type RuntimeLogEvent struct {
	Generation string `json:"generation"`
	ObservedAt int64  `json:"observedAt"`
	Level      int32  `json:"level"`
	Message    string `json:"message"`
	Reset      bool   `json:"reset,omitempty"`
}

// Stream lifetime/subscriber bounds belong to the existing observability owner.
// No second log ring is retained here. A restart cancels the old box context;
// callbacks are checked against the accepted pointer before delivery.
func (c *Core) SubscribeRuntimeLogs(ctx context.Context, generation string, emit func(RuntimeLogEvent) bool) error {
	if generation == "" {
		return ErrStaleGeneration
	}
	if c.logSubscribers.Add(1) > 4 {
		c.logSubscribers.Add(-1)
		return ErrRuntimeLimit
	}
	defer c.logSubscribers.Add(-1)
	c.access.RLock()
	api, instance := c.privateAPI, c.instance
	if !c.isRunning || api == nil || instance == nil {
		c.access.RUnlock()
		return ErrCoreUnavailable
	}
	if api.generation != generation {
		c.access.RUnlock()
		return ErrStaleGeneration
	}
	rpc, boxCtx := api.rpc, instance.Context()
	c.access.RUnlock()
	if rpc == nil {
		return ErrRuntimeAPIUnavailable
	}
	streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(boxCtx, cancel)
	defer stop()
	stream, err := rpc.SubscribeLog(streamCtx, &emptypb.Empty{})
	if err != nil {
		return ErrRuntimeAPIUnavailable
	}
	for {
		batch, err := stream.Recv()
		if err != nil {
			return ErrRuntimeAPIUnavailable
		}
		keep, err := c.deliverRuntimeLogs(api, batch, emit)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}
}

func (c *Core) deliverRuntimeLogs(api *privateAPI, batch *daemon.Log, emit func(RuntimeLogEvent) bool) (bool, error) {
	// Callbacks must enqueue without blocking. The short delivery lease ensures
	// a verified old batch cannot race publication of a replacement generation.
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	c.access.RLock()
	current := c.isRunning && c.privateAPI == api
	c.access.RUnlock()
	if !current {
		return false, ErrStaleGeneration
	}
	if len(batch.Messages) > 3000 {
		return false, ErrRuntimeLimit
	}
	if batch.Reset_ && !emit(RuntimeLogEvent{Generation: api.generation, ObservedAt: time.Now().UnixMilli(), Reset: true}) {
		return false, nil
	}
	for _, line := range batch.Messages {
		if !emit(RuntimeLogEvent{Generation: api.generation, ObservedAt: time.Now().UnixMilli(), Level: int32(line.Level), Message: redact.StringLimit(line.Message, 4096)}) {
			return false, nil
		}
	}
	return true, nil
}
