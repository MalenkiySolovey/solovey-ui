//go:build !linux

package updatebroker

import broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"

func RegisterHandlers(*broker.Registry) error { return nil }
