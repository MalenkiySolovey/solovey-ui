//go:build !linux || minimal

package privilegedbroker

func RunZapretEngineStop([]string) (bool, error) { return false, nil }
