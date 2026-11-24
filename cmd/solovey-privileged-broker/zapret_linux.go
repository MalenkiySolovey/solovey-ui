//go:build linux && !minimal

package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

func registerInstalledZapret(ctx context.Context, server *broker.Server, transport broker.TransportMode, listeners *broker.ListenerSet) error {
	return broker.RegisterInstalledZapret(ctx, server, transport, listeners, resolveZapretStrategies)
}

type installedStrategy struct{ strategy.Strategy }

func (value installedStrategy) LookupStrategy(ref string) (strategy.Strategy, bool) {
	return value.Strategy, ref == value.ID
}

func resolveZapretStrategies(inputs []json.RawMessage, requests []zapretactivation.Request) (broker.InstalledZapretStrategies, error) {
	if len(inputs) != len(requests) {
		return broker.InstalledZapretStrategies{}, errors.New("incomplete Zapret strategy set")
	}
	var result broker.InstalledZapretStrategies
	strategies := make([]strategy.Strategy, len(inputs))
	for i, request := range requests {
		var source installedStrategy
		if err := broker.DecodeRawPayload(inputs[i], &source.Strategy); err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		selected, err := binding.Resolve(outbounds.ZapretBinding{OutboundRef: request.OutboundRef, StrategyRef: request.StrategyRef, Enabled: true},
			binding.DirectOutbound{Host: hostref.Local(), Ref: request.OutboundRef, Type: "direct", Revision: request.OutboundRevision}, source, true)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		if selected.Intent.NeedsReturn {
			return broker.InstalledZapretStrategies{}, errors.New("installed Zapret supports outgoing observation only")
		}
		v4, err := selected.Strategy.PacketScopeForFamily(strategy.IPv4)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		v6, err := selected.Strategy.PacketScopeForFamily(strategy.IPv6)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		result.Intents = append(result.Intents, selected.Intent)
		result.Families = append(result.Families, planner.ZapretFamilies{
			IPv4: planner.ZapretPorts{TCP: v4.TCP, UDP: v4.UDP}, IPv6: planner.ZapretPorts{TCP: v6.TCP, UDP: v6.UDP},
			RequiresIPv6: slices.ContainsFunc(selected.Strategy.Profiles, func(p strategy.Profile) bool { return p.Family == strategy.IPv6 }),
		})
		strategies[i] = selected.Strategy
		result.UsesCustomBlobs = result.UsesCustomBlobs || len(selected.Strategy.BlobData) != 0
	}
	result.HandlesQUIC = func(i int, ipv6 bool, port uint16) bool {
		family := strategy.IPv4
		if ipv6 {
			family = strategy.IPv6
		}
		return strategies[i].HandlesQUIC(family, port)
	}
	result.Compile = func(root string, queue uint16, loopMark uint32, marks []uint32, trace bool) ([]string, broker.ZapretNativeTrace, error) {
		if len(marks) != len(strategies) {
			return nil, nil, errors.New("incomplete Zapret mark allocation")
		}
		marked := make([]strategy.MarkedStrategy, len(marks))
		for i, mark := range marks {
			marked[i] = strategy.MarkedStrategy{Strategy: strategies[i], QueueMark: mark}
		}
		if trace {
			return strategy.CompileBindingsWithNativeTrace(root, queue, loopMark, marked)
		}
		args, err := strategy.CompileBindings(root, queue, loopMark, marked)
		return args, nil, err
	}
	return result, nil
}
