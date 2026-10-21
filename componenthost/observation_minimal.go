//go:build minimal

package componenthost

type ObservationPorts struct{}

func (d APIDeps) WithObservationOwners() APIDeps        { return d }
func (APIDeps) bindObservationPort(string, *Ports) bool { return false }
