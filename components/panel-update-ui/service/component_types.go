package service

import "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

type Group string

const (
	GroupInstalled   Group = "installed"
	GroupAvailable   Group = "available"
	GroupUnavailable Group = "unavailable"
)

type Inventory struct {
	BinaryProfile string            `json:"binaryProfile"`
	Components    []ComponentStatus `json:"components"`
	Installed     []ComponentStatus `json:"installed"`
	Available     []ComponentStatus `json:"available"`
	Unavailable   []ComponentStatus `json:"unavailable"`
}

type ComponentStatus struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Version           string            `json:"version"`
	LatestVersion     string            `json:"latestVersion,omitempty"`
	Since             string            `json:"since,omitempty"`
	Delivery          manifest.Delivery `json:"delivery"`
	DefaultEnabled    bool              `json:"defaultEnabled"`
	TokenScopes       []string          `json:"tokenScopes,omitempty"`
	AvailableInBinary bool              `json:"availableInBinary"`
	Installable       bool              `json:"installable"`
	Removable         bool              `json:"removable"`
	Installed         bool              `json:"installed"`
	Enabled           bool              `json:"enabled"`
	Active            bool              `json:"active"`
	Group             Group             `json:"group"`
	UnavailableReason string            `json:"unavailableReason,omitempty"`
}

type OperationContext struct {
	Actor    string
	Hostname string
}
