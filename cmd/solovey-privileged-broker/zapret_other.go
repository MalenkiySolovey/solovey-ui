//go:build !linux || minimal

package main

import (
	"context"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func registerInstalledZapret(context.Context, *broker.Server, broker.TransportMode, *broker.ListenerSet) error {
	return nil
}
