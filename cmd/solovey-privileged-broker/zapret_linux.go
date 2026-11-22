//go:build linux && !minimal

package main

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func registerInstalledZapret(ctx context.Context, server *broker.Server, transport broker.TransportMode, listeners *broker.ListenerSet) error {
	return broker.RegisterInstalledZapret(ctx, server, transport, listeners, resolveZapretStrategy)
}

type installedStrategy struct{ strategy.Strategy }

func (value installedStrategy) LookupStrategy(ref string) (strategy.Strategy, bool) {
	return value.Strategy, ref == value.ID
}

func resolveZapretStrategy(input json.RawMessage, request zapretactivation.Request) (broker.InstalledZapretStrategy, error) {
	var source installedStrategy
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source.Strategy); err != nil {
		return broker.InstalledZapretStrategy{}, err
	}
	selected, err := binding.Resolve(outbounds.ZapretBinding{OutboundRef: request.OutboundRef, StrategyRef: request.StrategyRef, Enabled: true},
		binding.DirectOutbound{Host: hostref.Local(), Ref: request.OutboundRef, Type: "direct", Revision: request.OutboundRevision}, source, true)
	if err != nil {
		return broker.InstalledZapretStrategy{}, err
	}
	return broker.InstalledZapretStrategy{Intent: selected.Intent, Compile: selected.Strategy.Compile}, nil
}
