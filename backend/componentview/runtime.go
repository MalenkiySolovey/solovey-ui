package componentview

import "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"

// RuntimeInventory is the existing installed-only API inventory. Catalog
// installation candidates remain the separate Inventory projection.
type RuntimeInventory struct {
	BinaryProfile string             `json:"binaryProfile"`
	Components    []RuntimeComponent `json:"components"`
}

type RuntimeComponent struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Version           string               `json:"version"`
	Since             string               `json:"since,omitempty"`
	Delivery          manifest.Delivery    `json:"delivery"`
	DefaultEnabled    bool                 `json:"defaultEnabled"`
	TokenScopes       []string             `json:"tokenScopes,omitempty"`
	Installed         bool                 `json:"installed"`
	Enabled           bool                 `json:"enabled"`
	Active            bool                 `json:"active"`
	AvailableInBinary bool                 `json:"availableInBinary"`
	ObservedLifecycle LifecycleObservation `json:"observedLifecycle"`
}
