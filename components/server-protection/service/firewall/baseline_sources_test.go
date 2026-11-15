package firewall

import (
	"context"
	"errors"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestMissingBaselineFactOwnerRejectsBeforeAnySourceOrStateRead(t *testing.T) {
	for _, missing := range []string{"inventory", "surfaces", "targets", "endpoints", "recovery", "effective"} {
		t.Run(missing, func(t *testing.T) {
			sources := unreadBaselineSources(t)
			switch missing {
			case "inventory":
				sources.Inventory = nil
			case "surfaces":
				sources.Surfaces = nil
			case "targets":
				sources.Targets = nil
			case "endpoints":
				sources.Endpoints = nil
			case "recovery":
				sources.RecoveryEvidence = nil
			case "effective":
				sources.EffectiveRecovery = nil
			}
			service := NewBaselineService(struct{ BaselineStore }{}, struct{ SSHPostureSource }{}, struct{ NFTCodec }{}, sources)
			if _, err := service.Snapshot(t.Context(), true, nil); !errors.Is(err, ErrMissingCapability) {
				t.Fatalf("incomplete fact owner admitted: %v", err)
			}
			if _, err := service.RuntimeManagement(t.Context()); !errors.Is(err, ErrWorkflowDisabled) {
				t.Fatalf("runtime read incomplete owners: %v", err)
			}
		})
	}
}

// Every port is present, but any observation before admission fails the test.
func unreadBaselineSources(t *testing.T) BaselineSources {
	t.Helper()
	return BaselineSources{Inventory: struct {
		protectionresources.InventoryReader
	}{}, Surfaces: struct{ SurfaceSource }{},
		Targets: func(context.Context, time.Time) []BaselineTargetFact {
			t.Fatal("read targets before owner admission")
			return nil
		},
		Endpoints: func([]hostresources.ProtectableResource, hostfacts.Snapshot, time.Time) []hostresources.ManagementEndpointV1 {
			t.Fatal("read management before owner admission")
			return nil
		},
		RecoveryEvidence: func(context.Context, time.Time) BaselineRecoveryEvidence {
			t.Fatal("read recovery before owner admission")
			return BaselineRecoveryEvidence{}
		},
		EffectiveRecovery: func(hostresources.RecoveryPathV1, []hostresources.ManagementEndpointV1, time.Time) hostresources.RecoveryPathV1 {
			t.Fatal("admitted recovery before owner admission")
			return hostresources.RecoveryPathV1{}
		},
	}
}
