//go:build minimal

package componenthost

import hostports "github.com/MalenkiySolovey/solovey-host/componenthost"

type ObservationPorts struct{}

func (APIDeps) bindObservationPort(string, *Ports) bool { return false }

func (Ports) bindLocalObservation(*hostports.Ports) {}
