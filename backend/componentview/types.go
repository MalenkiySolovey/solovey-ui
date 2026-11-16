// Package componentview defines the authenticated Component HTTP projections.
package componentview

import (
	"context"
	"github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"github.com/MalenkiySolovey/solovey-ui/backend/actions"
	"time"
)

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
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Version           string               `json:"version"`
	LatestVersion     string               `json:"latestVersion,omitempty"`
	Since             string               `json:"since,omitempty"`
	RequiredPanel     string               `json:"requiredPanelVersion,omitempty"`
	Delivery          manifest.Delivery    `json:"delivery"`
	DefaultEnabled    bool                 `json:"defaultEnabled"`
	TokenScopes       []string             `json:"tokenScopes,omitempty"`
	AvailableInBinary bool                 `json:"availableInBinary"`
	Compatible        bool                 `json:"compatible"`
	Locked            bool                 `json:"locked,omitempty"`
	LockedReason      string               `json:"lockedReason,omitempty"`
	Installable       bool                 `json:"installable"`
	Removable         bool                 `json:"removable"`
	Installed         bool                 `json:"installed"`
	Enabled           bool                 `json:"enabled"`
	Active            bool                 `json:"active"`
	ObservedLifecycle LifecycleObservation `json:"observedLifecycle"`
	Group             Group                `json:"group"`
	UnavailableReason string               `json:"unavailableReason,omitempty"`
}

type OperationContext struct {
	Authorize      actions.Authorizer
	RequestContext context.Context
	Actor          string
	Hostname       string
}

// LifecycleObservation projects the existing supervisor result without owning
// lifecycle transitions, health decisions or instance retention.
type LifecycleObservation struct {
	Phase             string     `json:"phase"`
	InstanceRetention string     `json:"instanceRetention"`
	Source            string     `json:"source"`
	ObservedAt        *time.Time `json:"observedAt,omitempty"`
	ReasonCode        string     `json:"reasonCode"`
}
