package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

// PrepareDialerOverride validates against the selected SDK without changing
// explicit false/zero values, which can intentionally override source values.
func PrepareDialerOverride(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	invalid := errors.New("SUBSCRIPTION_SOURCE_DIAL_OPTIONS_INVALID")
	var fields map[string]json.RawMessage
	if len(raw) > 65536 || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, invalid
	}
	if len(fields) == 0 {
		return nil, nil
	}
	allowed := jsonfields.Names(reflect.TypeFor[option.DialerOptions](), false)
	for key, value := range fields {
		if !slices.Contains(allowed, key) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, invalid
		}
		if key == "domain_resolver" && len(value) > 0 && bytes.TrimSpace(value)[0] == '{' {
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) != nil {
				return nil, invalid
			}
			for name := range nested {
				if !slices.Contains(jsonfields.Names(reflect.TypeFor[option.DomainResolveOptions](), false), name) {
					return nil, invalid
				}
			}
		}
	}
	var typed option.DialerOptions
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil {
		return nil, invalid
	}
	return json.Marshal(fields)
}

// ApplyDialerOverride changes the derived outbound only. Group and other
// consumers without a DialerOptions contract retain their own semantics.
func ApplyDialerOverride(runtimeType string, outbound, override json.RawMessage) (json.RawMessage, error) {
	prepared, err := PrepareDialerOverride(override)
	if err != nil || len(prepared) == 0 {
		return outbound, err
	}
	consumer, exists := registry.OutboundRegistry().CreateOptions(runtimeType)
	if !exists {
		return nil, errors.New("SUBSCRIPTION_SOURCE_DIAL_CONSUMER_UNAVAILABLE")
	}
	if _, supported := consumer.(option.DialerOptionsWrapper); !supported {
		return outbound, nil
	}
	var fields, overrides map[string]json.RawMessage
	if json.Unmarshal(outbound, &fields) != nil || fields == nil {
		return nil, errors.New("SUBSCRIPTION_SOURCE_OUTBOUND_INVALID")
	}
	_ = json.Unmarshal(prepared, &overrides)
	for key, value := range overrides {
		fields[key] = value
	}
	return json.Marshal(fields)
}

func (c *Core) ValidateOutbound(tag string) error {
	return c.withRuntime(func(current coreRuntime) error {
		if _, ok := current.outboundManager.Outbound(tag); !ok {
			return ErrOutboundNotFound
		}
		return nil
	})
}

func (c *Core) DialOutbound(ctx context.Context, tag, network string, destination M.Socksaddr) (connection net.Conn, err error) {
	var selected adapter.Outbound
	var generation context.Context
	err = c.withRuntime(func(current coreRuntime) error {
		outbound, ok := current.outboundManager.Outbound(tag)
		if !ok {
			return ErrOutboundNotFound
		}
		selected, generation = outbound, current.ctx
		c.outboundDials.Add(1)
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer c.outboundDials.Done()

	// Network I/O must not hold the lease that Stop needs to retire the Box.
	// Keep caller cancellation and bind the admitted dial to its generation.
	dialContext, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(generation, cancel)
	defer func() { stopCancellation(); cancel() }()
	if generation.Err() != nil {
		return nil, ErrCoreUnavailable
	}
	connection, err = selected.DialContext(dialContext, network, destination)
	if generation.Err() != nil {
		if connection != nil {
			_ = connection.Close()
		}
		return nil, ErrCoreUnavailable
	}
	return connection, err
}
