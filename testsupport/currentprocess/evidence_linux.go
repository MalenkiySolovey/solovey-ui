//go:build linux

// Package currentprocess supplies read-only evidence for the integration test
// process through the existing platform evidence owners.
package currentprocess

import (
	"context"
	"os"

	listeners "github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	process "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

type (
	Fact        = process.Fact
	Observation = listeners.Observation
)

func Current() (Fact, error) { return process.Observe(os.Getpid()) }
func AcceptingTCP(ctx context.Context, allowedPorts map[uint16]bool) (Observation, error) {
	return listeners.ObserveAcceptingTCPDetailed(ctx, os.Getpid(), allowedPorts)
}
