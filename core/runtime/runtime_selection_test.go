package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	hostcore "github.com/MalenkiySolovey/solovey-host/coreruntime"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/sagernet/sing-box/daemon"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Go test binaries do not carry the complete production module build-info
// identity. Use the accepted catalog for this exact compiled test flavor; a
// separate normal binary qualification exercises Current without this seam.
func configureSelectionProvider(t *testing.T, c *Core) {
	t.Helper()
	data, err := os.ReadFile("../provider/pins.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Sources []coreprovider.Source }
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, source := range catalog.Sources {
		if source.ID == selectionFixtureProvider {
			identity := coreprovider.ResolveModule(source.Module, source.ProviderVersion, source.ModuleSum)
			if !coreprovider.SourceIdentityVerified(identity) {
				t.Fatal("unverified fixture provider")
			}
			c.selectionProvider = func() coreprovider.Identity { return identity }
			return
		}
	}
	t.Fatal("compiled fixture provider absent from accepted catalog")
}

type selectionReplyClient struct {
	daemon.StartedServiceClient
	mode   string
	calls  int
	cancel context.CancelFunc
}

func (c *selectionReplyClient) SelectOutbound(ctx context.Context, request *daemon.SelectOutboundRequest, options ...grpc.CallOption) (*emptypb.Empty, error) {
	c.calls++
	if c.calls == 1 && (c.mode == "failed-verification" || c.mode == "rollback-failure") {
		return &emptypb.Empty{}, nil
	}
	if c.calls == 2 && c.mode == "rollback-failure" {
		return nil, errors.New("injected recovery failure")
	}
	result, err := c.StartedServiceClient.SelectOutbound(ctx, request, options...)
	if c.calls == 1 {
		if c.mode == "ambiguous-apply" {
			return nil, errors.New("injected lost acknowledgment")
		}
		if c.mode == "cancel-after-apply" {
			c.cancel()
			return nil, ctx.Err()
		}
	}
	return result, err
}

func TestRuntimeSelectionSharesApplyVerifyRecoveryWithActualProvider(t *testing.T) {
	for _, mode := range []string{"success", "failed-verification", "ambiguous-apply", "rollback-failure", "cancel-after-apply"} {
		t.Run(mode, func(t *testing.T) {
			core := startPrivateFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &selectionReplyClient{StartedServiceClient: core.privateAPI.rpc, mode: mode, cancel: cancel}
			core.privateAPI.rpc = client
			generation := core.privateAPI.generation
			result, err := core.SelectRuntimeGroupWithEvidence(ctx, generation, "choice", "second")
			if mode == "success" {
				if err != nil || result.Outcome != hostcore.SelectionVerified || result.Observed != "second" || result.Health.Proof != "RUNTIME_SELECTION_VERIFIED" {
					t.Fatalf("%+v %v", result, err)
				}
				again, err := core.SelectRuntimeGroupWithEvidence(ctx, generation, "choice", "second")
				if err != nil || again.ApplyAttempted || client.calls != 1 {
					t.Fatal("duplicate runtime mutation", err, client.calls)
				}
				return
			}
			if err == nil {
				t.Fatal("failed operation became success")
			}
			if mode == "rollback-failure" {
				if result.Outcome != hostcore.SelectionAmbiguous || result.Health.State != "UNKNOWN" {
					t.Fatalf("failed recovery became ready: %+v", result)
				}
				return
			}
			if result.Outcome != hostcore.SelectionRestored || result.Observed != "direct" || result.Health.Proof != "RUNTIME_SELECTION_RESTORED" || client.calls != 2 {
				t.Fatalf("unverified recovery: %+v %v calls=%d", result, err, client.calls)
			}
			groups, readErr := core.Groups(context.Background(), generation)
			if readErr != nil || groups.Groups[0].Selected != "direct" {
				t.Fatal("actual provider before-image was not restored", readErr)
			}
		})
	}
}

func TestRuntimeSelectionWaitingForExistingLeaseHonorsCancellation(t *testing.T) {
	core := startPrivateFixture(t)
	identity := core.selectionIdentity()
	entered := make(chan struct{})
	var once sync.Once
	core.selectionProvider = func() coreprovider.Identity { once.Do(func() { close(entered) }); return identity }
	core.mutation.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- core.SelectRuntimeGroup(ctx, core.privateAPI.generation, "choice", "second") }()
	<-entered
	cancel()
	core.mutation.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled operation did not finish after lease release")
	}
}

func TestRuntimeSelectionUnknownProviderNeverDispatches(t *testing.T) {
	core := startPrivateFixture(t)
	client := &selectionReplyClient{StartedServiceClient: core.privateAPI.rpc}
	core.privateAPI.rpc = client
	core.selectionProvider = func() coreprovider.Identity { return coreprovider.Identity{State: coreprovider.Unknown} }
	if _, err := core.SelectRuntimeGroupWithEvidence(context.Background(), core.privateAPI.generation, "choice", "second"); err == nil || client.calls != 0 {
		t.Fatal("unknown provider acquired mutation capability")
	}
}
