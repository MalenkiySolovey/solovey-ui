//go:build !minimal

package datalifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
)

type dropDataFixtureLifecycle struct{ lifecycle.Noop }

func (dropDataFixtureLifecycle) DropData(context.Context, lifecycle.Context) error { return nil }

func registerDropDataFixtureOwner() componentmanifest.Manifest {
	const id = "drop-owner-fixture"
	if item, available := durableowner.Lookup(id); available {
		return item
	}
	item := componentmanifest.Manifest{
		ID: id, Name: "Drop owner fixture", Version: "1", Delivery: componentmanifest.DeliveryInProcess,
		Database: componentmanifest.Database{
			Tables:   []string{"drop_owner_fixture_rows", "drop_owner_fixture_events"},
			Settings: []string{"dropOwnerFixtureEnabled"}, Secrets: []string{"dropOwnerFixtureSecret"},
		},
	}.Normalized()
	componentregistry.Register(componentregistry.Component{Manifest: item, Lifecycle: dropDataFixtureLifecycle{}})
	return item
}

func dataLifecycleTestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func portableDropRecoveryFilename(operationID string) string {
	return "drop-" + dataLifecycleTestDigest(operationID) + ".db"
}
func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
