package manifest

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"

func FromJSON(data []byte) (Manifest, error) { return canonical.FromJSON(data) }
func MustFromJSON(data []byte) Manifest      { return canonical.MustFromJSON(data) }
