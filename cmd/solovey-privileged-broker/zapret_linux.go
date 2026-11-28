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

func resolveZapretBatch(inputs []json.RawMessage, requests []zapretactivation.Request, luaSources map[string]json.RawMessage, reserve func(bytes, lines int) error) (broker.InstalledZapretStrategies, error) {
	result, err := resolveZapretStrategies(inputs, requests, luaSources, reserve)
	if err != nil {
		return result, err
	}
	compile := result.Compile
	result.Compile = func(root, listDir string, queue uint16, loopMark uint32, marks []uint32, trace, replies bool) (broker.ZapretEngineCommand, error) {
		command, err := compile(root, listDir, queue, loopMark, marks, trace, replies)
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

func resolveZapretStrategies(inputs []json.RawMessage, requests []zapretactivation.Request, luaSources map[string]json.RawMessage, reserve func(bytes, lines int) error) (broker.InstalledZapretStrategies, error) {
	if len(inputs) != len(requests) {
		return broker.InstalledZapretStrategies{}, errors.New("incomplete Zapret strategy set")
	}
	var result broker.InstalledZapretStrategies
	strategies := make([]strategy.Strategy, len(inputs))
	sources := make([][]strategy.LuaModule, len(inputs))
	usedSources := make(map[string]bool, len(luaSources))
	for i, request := range requests {
		source, err := decodeInstalledZapretStrategy(inputs[i], reserve)
		if err != nil {
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
			return broker.InstalledZapretStrategies{}, errors.New("only editor-managed strategies can select Lua sources")
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
	result.Compile = func(root, listDir string, queue uint16, loopMark uint32, marks []uint32, trace, replies bool) (broker.ZapretEngineCommand, error) {
		if len(marks) != len(strategies) {
			return broker.ZapretEngineCommand{}, errors.New("incomplete Zapret mark allocation")
		}
		marked := make([]strategy.MarkedStrategy, len(marks))
		for i, mark := range marks {
			marked[i] = strategy.MarkedStrategy{Strategy: strategies[i], QueueMark: mark}
		}
		var selectedLua [][]strategy.LuaModule
		if usesManagedLua {
			selectedLua = sources
		}
		material, err := strategy.CompileInstalledBindings(root, listDir, broker.ZapretLuaBundlePath, queue, loopMark, marked, selectedLua, trace, replies)
		if err != nil {
			return broker.ZapretEngineCommand{}, err
		}
		command := broker.ZapretEngineCommand{Args: material.Args, Lua: material.Lua, ListFiles: material.ListFiles}
		if material.Trace != nil {
			command.Trace = material.Trace
		}
		return command, nil
	}
	return result, nil
}

func decodeInstalledZapretStrategy(raw json.RawMessage, reserve func(bytes, lines int) error) (installedStrategy, error) {
	var wire struct {
		strategy.Strategy
		Profiles []struct {
			strategy.Profile
			Packed *struct {
				HostList        *zapretactivation.PackedList `json:"hostList,omitempty"`
				HostListExclude *zapretactivation.PackedList `json:"hostListExclude,omitempty"`
				IPSet           *zapretactivation.PackedList `json:"ipSet,omitempty"`
				IPSetExclude    *zapretactivation.PackedList `json:"ipSetExclude,omitempty"`
			} `json:"packedLists,omitempty"`
		} `json:"profiles"`
	}
	if err := broker.DecodeRawPayload(raw, &wire); err != nil {
		return installedStrategy{}, err
	}
	wire.Strategy.Profiles = make([]strategy.Profile, 0, len(wire.Profiles))
	for _, profile := range wire.Profiles {
		value := profile.Profile
		if profile.Packed != nil {
			if value.Lists != nil {
				return installedStrategy{}, errors.New("ambiguous installed Zapret list input")
			}
			lists := new(strategy.NativeLists)
			for i, input := range [...]*zapretactivation.PackedList{
				profile.Packed.HostList, profile.Packed.HostListExclude, profile.Packed.IPSet, profile.Packed.IPSetExclude,
			} {
				if input == nil {
					continue
				}
				entries, err := input.Expand(reserve)
				if err != nil {
					return installedStrategy{}, err
				}
				*([4]*[]string{&lists.HostList, &lists.HostListExclude, &lists.IPSet, &lists.IPSetExclude})[i] = entries
			}
			value.Lists = lists
		}
		wire.Strategy.Profiles = append(wire.Strategy.Profiles, value)
	}
	return installedStrategy{wire.Strategy}, nil
}

func zapretPlannerPorts(scope strategy.Scope) planner.ZapretPorts {
	return planner.ZapretPorts{TCP: scope.TCP, UDP: scope.UDP, ReplyTCP: scope.ReturnTCP,
		TCPRanges: scope.TCPRanges, UDPRanges: scope.UDPRanges, ReplyTCPRanges: scope.ReturnTCPRanges}
}
