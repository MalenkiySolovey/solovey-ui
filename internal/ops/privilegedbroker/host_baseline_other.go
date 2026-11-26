//go:build !linux || minimal

package privilegedbroker

func RegisterHostBaselineObserver(*Registry, TransportMode) error { return nil }
