package resources

import (
	"context"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type InventorySnapshot struct {
	GeneratedAt int64                               `json:"generatedAt"`
	Resources   []hostresources.ProtectableResource `json:"resources"`
	Collisions  []Collision                         `json:"collisions,omitempty"`
	Warnings    []hostresources.ResourceWarning     `json:"warnings,omitempty"`
	Errors      []hostresources.ResourceError       `json:"errors,omitempty"`
}

func Snapshot(ctx context.Context, refresh bool) InventorySnapshot {
	var source hostresources.ResourceSnapshot
	if refresh {
		source = hostresources.Refresh(ctx)
	} else {
		source = hostresources.Snapshot(ctx)
	}
	return FromHostSnapshot(source)
}

func FromHostSnapshot(source hostresources.ResourceSnapshot) InventorySnapshot {
	return InventorySnapshot{
		GeneratedAt: source.GeneratedAt,
		Resources:   append([]hostresources.ProtectableResource(nil), source.Resources...),
		Collisions:  DetectCollisions(source.Resources),
		Warnings:    append([]hostresources.ResourceWarning(nil), source.Warnings...),
		Errors:      append([]hostresources.ResourceError(nil), source.Errors...),
	}
}
