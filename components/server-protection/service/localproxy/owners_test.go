package localproxy

import (
	"context"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

type surfaceOwnerFixture struct{ calls int }

func (s *surfaceOwnerFixture) Reconcile(context.Context) hostsurface.Snapshot {
	s.calls++
	return hostsurface.Snapshot{}
}

func TestMissingOwnerRejectsBeforeReadingStateOrRefreshingSurface(t *testing.T) {
	for _, missing := range []string{"providers", "probes", "surface"} {
		t.Run(missing, func(t *testing.T) {
			// These nonnil owners panic on any invocation. Admission must finish first.
			c := &Controller{Repository: struct{ StateRepository }{}, Operations: struct{ operationManager }{}, Providers: struct{ ProviderDirectory }{}, Probes: struct{ ProbeSource }{}, Surface: struct{ SurfaceSource }{}}
			switch missing {
			case "providers":
				c.Providers = nil
			case "probes":
				c.Probes = nil
			case "surface":
				c.Surface = nil
			}
			if _, err := c.Status(t.Context(), true); ErrorCode(err) != CodeInternalFailure {
				t.Fatalf("missing owner admitted: %v", err)
			}
		})
	}
}

type emptyStateOwner struct {
	StateRepository
	reads int
}

func (s *emptyStateOwner) LocalProxyStates(context.Context) ([]durable.LocalProxyStateV1Model, error) {
	s.reads++
	return nil, nil
}

type emptyProviderOwner struct {
	ProviderDirectory
	reads int
}

func (s *emptyProviderOwner) FactsV1(context.Context, time.Time) ([]hostresources.LocalProxyFactV1, error) {
	s.reads++
	return nil, nil
}

func TestStatusRefreshUsesOnlyExplicitSurfaceOwner(t *testing.T) {
	state, providers, surface := &emptyStateOwner{}, &emptyProviderOwner{}, &surfaceOwnerFixture{}
	c := &Controller{Repository: state, Operations: struct{ operationManager }{}, Providers: providers, Probes: struct{ ProbeSource }{}, Surface: surface}
	for _, refresh := range []bool{false, true} {
		if _, err := c.Status(t.Context(), refresh); err != nil {
			t.Fatal(err)
		}
	}
	if surface.calls != 1 || state.reads != 2 || providers.reads != 2 {
		t.Fatalf("wrong explicit owner calls: surface=%d state=%d providers=%d", surface.calls, state.reads, providers.reads)
	}
}
