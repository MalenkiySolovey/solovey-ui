//go:build linux && !minimal

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	C "github.com/sagernet/sing-box/constant"
)

func registerInstalledZapret(ctx context.Context, server *broker.Server, transport broker.TransportMode, listeners *broker.ListenerSet) error {
	return broker.RegisterInstalledZapret(ctx, server, transport, listeners, resolveZapretBatch)
}

func resolveZapretBatch(inputs []json.RawMessage, requests []zapretactivation.Request, luaSources map[string]json.RawMessage) (broker.InstalledZapretStrategies, error) {
	result, err := resolveZapretStrategies(inputs, requests, luaSources)
	if err != nil {
		return result, err
	}
	compile := result.Compile
	result.Compile = func(root string, queue uint16, loopMark uint32, marks []uint32, trace, replies bool) (broker.ZapretEngineCommand, error) {
		command, err := compile(root, queue, loopMark, marks, trace, replies)
		if errors.Is(err, strategy.ErrEngineArgumentBudget) {
			return broker.ZapretEngineCommand{}, fmt.Errorf("%w: %w", broker.ErrZapretEngineBudget, err)
		}
		return command, err
	}
	return result, nil
}

type installedStrategy struct{ strategy.Strategy }

func (value installedStrategy) LookupStrategy(ref string) (strategy.Strategy, bool) {
	return value.Strategy, ref == value.ID
}

func resolveZapretStrategies(inputs []json.RawMessage, requests []zapretactivation.Request, luaSources map[string]json.RawMessage) (broker.InstalledZapretStrategies, error) {
	if len(inputs) != len(requests) {
		return broker.InstalledZapretStrategies{}, errors.New("incomplete Zapret strategy set")
	}
	var result broker.InstalledZapretStrategies
	strategies := make([]strategy.Strategy, len(inputs))
	sources := make([][]strategy.LuaModule, len(inputs))
	usedSources := make(map[string]bool, len(luaSources))
	for i, request := range requests {
		var source installedStrategy
		if err := broker.DecodeRawPayload(inputs[i], &source.Strategy); err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		outboundType := request.Outer.Kind
		if outboundType == "" {
			outboundType = C.TypeDirect
		}
		selected, err := binding.Resolve(outbounds.ZapretBinding{OutboundRef: request.OutboundRef, StrategyRef: request.StrategyRef, Enabled: true},
			binding.DirectOutbound{Host: hostref.Local(), Ref: request.OutboundRef, Type: outboundType, Revision: request.OutboundRevision}, source, true)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
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
			IPv4:         zapretPlannerPorts(v4),
			IPv6:         zapretPlannerPorts(v6),
			RequiresIPv6: slices.ContainsFunc(selected.Strategy.Profiles, func(p strategy.Profile) bool { return p.Family == strategy.IPv6 }),
		})
		strategies[i] = selected.Strategy
		result.UsesCustomBlobs = result.UsesCustomBlobs || len(selected.Strategy.BlobData) != 0
		refs := selected.Strategy.ManagedLua
		if len(refs) == 0 {
			continue
		}
		if !zapretactivation.ManagedStrategyRef(request.StrategyRef) {
			return broker.InstalledZapretStrategies{}, errors.New("Lua sources can only be selected by an editor-managed strategy")
		}
		catalog := strategy.Catalog{LuaRevisions: make(map[string]strategy.LuaDraft, len(refs))}
		for _, ref := range refs {
			key := zapretactivation.LuaSourceKey(ref.ID, ref.Revision)
			var draft strategy.LuaDraft
			if err := broker.DecodeRawPayload(luaSources[key], &draft); err != nil {
				return broker.InstalledZapretStrategies{}, errors.New("missing or invalid selected Zapret Lua source")
			}
			catalog.LuaRevisions[key] = draft
			usedSources[key] = true
		}
		sources[i], err = catalog.SelectedLua(refs)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		result.UsesManagedLua = true
	}
	if len(usedSources) != len(luaSources) {
		return broker.InstalledZapretStrategies{}, errors.New("installed Zapret received unselected Lua sources")
	}
	usesManagedLua := result.UsesManagedLua
	result.HandlesQUIC = func(i int, ipv6 bool, port uint16) bool {
		family := strategy.IPv4
		if ipv6 {
			family = strategy.IPv6
		}
		return strategies[i].HandlesQUIC(family, port)
	}
	result.Compile = func(root string, queue uint16, loopMark uint32, marks []uint32, trace, replies bool) (broker.ZapretEngineCommand, error) {
		if len(marks) != len(strategies) {
			return broker.ZapretEngineCommand{}, errors.New("incomplete Zapret mark allocation")
		}
		marked := make([]strategy.MarkedStrategy, len(marks))
		for i, mark := range marks {
			marked[i] = strategy.MarkedStrategy{Strategy: strategies[i], QueueMark: mark}
		}
		var command broker.ZapretEngineCommand
		var evidence *strategy.NativeTrace
		var err error
		switch {
		case usesManagedLua:
			command.Args, evidence, command.Lua, err = strategy.CompileManagedBindings(root, broker.ZapretLuaBundlePath, queue, loopMark, marked, sources, trace, replies)
		case replies:
			command.Args, evidence, err = strategy.CompileBindingsForReturnPath(root, queue, loopMark, marked, trace)
		case trace:
			command.Args, evidence, err = strategy.CompileBindingsWithNativeTrace(root, queue, loopMark, marked)
		default:
			command.Args, err = strategy.CompileBindings(root, queue, loopMark, marked)
		}
		if evidence != nil {
			command.Trace = evidence
		}
		return command, err
	}
	return result, nil
}

func zapretPlannerPorts(scope strategy.Scope) planner.ZapretPorts {
	return planner.ZapretPorts{TCP: scope.TCP, UDP: scope.UDP, ReplyTCP: scope.ReturnTCP,
		TCPRanges: scope.TCPRanges, UDPRanges: scope.UDPRanges, ReplyTCPRanges: scope.ReturnTCPRanges}
}
