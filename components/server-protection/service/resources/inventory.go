package resources

import (
	"context"

	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

type InventorySnapshot struct {
	GeneratedAt int64                               `json:"generatedAt"`
	Resources   []hostresources.ProtectableResource `json:"resources"`
	Collisions  []Collision                         `json:"collisions,omitempty"`
	Warnings    []hostresources.ResourceWarning     `json:"warnings,omitempty"`
	Errors      []hostresources.ResourceError       `json:"errors,omitempty"`
}

// InventoryReader is the existing owner's read-only inventory view. The feature
// receives it from composition and owns no registration, cache or discovery.
type InventoryReader interface {
	Snapshot(context.Context) hostresources.ResourceSnapshot
	Refresh(context.Context) hostresources.ResourceSnapshot
}

func Snapshot(ctx context.Context, refresh bool, reader InventoryReader) InventorySnapshot {
	if reader == nil {
		return FromHostSnapshot(hostresources.ResourceSnapshot{Errors: []hostresources.ResourceError{{Owner: "registry", Message: "resource inventory is unavailable"}}})
	}
	var source hostresources.ResourceSnapshot
	if refresh {
		source = reader.Refresh(ctx)
	} else {
		source = reader.Snapshot(ctx)
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
