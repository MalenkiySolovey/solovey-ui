//go:build linux && !minimal

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
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
		decoder := json.NewDecoder(bytes.NewReader(inputs[i]))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&source.Strategy); err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		selected, err := binding.Resolve(outbounds.ZapretBinding{OutboundRef: request.OutboundRef, StrategyRef: request.StrategyRef, Enabled: true},
			binding.DirectOutbound{Host: hostref.Local(), Ref: request.OutboundRef, Type: "direct", Revision: request.OutboundRevision}, source, true)
		if err != nil {
			return broker.InstalledZapretStrategies{}, err
		}
		if selected.Intent.NeedsReturn || len(selected.Intent.TCPPorts) == 0 || len(selected.Intent.UDPPorts) != 0 {
			return broker.InstalledZapretStrategies{}, errors.New("installed Zapret requires a static outgoing TCP strategy")
		}
		result.Intents = append(result.Intents, selected.Intent)
		strategies[i] = selected.Strategy
	}
	result.Compile = func(root string, queue uint16, loopMark uint32, marks []uint32) ([]string, error) {
		if len(marks) != len(strategies) {
			return nil, errors.New("incomplete Zapret mark allocation")
		}
		marked := make([]strategy.MarkedStrategy, len(marks))
		for i, mark := range marks {
			marked[i] = strategy.MarkedStrategy{Strategy: strategies[i], QueueMark: mark}
		}
		return strategy.CompileBindings(root, queue, loopMark, marked)
	}
	return result, nil
}
