package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

// Tests supply real owner projections from the shared catalog, since test
// executables may omit dependency build metadata. Production uses Current.
func updateTestCoreProvider(t testing.TB, id string) func() coreprovider.Identity {
	t.Helper()
	identity := providerfixture.Identity(t, id)
	return func() coreprovider.Identity { return identity }
}

type providerGuardReleaseClient struct {
	ReleaseClient
	calls int
}

func (client *providerGuardReleaseClient) Fetch(ctx context.Context, source release.Source) ([]byte, error) {
	client.calls++
	return client.ReleaseClient.Fetch(ctx, source)
}

func TestLegacyReleaseCannotAdmitUnknownOrExtendedCoreProvider(t *testing.T) {
	for _, test := range []struct {
		name string
		id   string
		want error
	}{
		{"extended", "shtorm-extended", ErrCoreProviderMaintenance},
		{"unknown", "", ErrCoreProviderUnverified},
		{"forged-official", "forged", ErrCoreProviderUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			identity := coreprovider.Resolve(nil)
			if test.id != "" {
				fixtureID := test.id
				if fixtureID == "forged" {
					fixtureID = coreprovider.DefaultID()
				}
				identity = updateTestCoreProvider(t, fixtureID)()
				if test.id == "forged" {
					identity.SourceRevision = "credential-sentinel-untrusted"
				}
			}
			fixture.manager.coreProvider = func() coreprovider.Identity { return identity }
			client := &providerGuardReleaseClient{ReleaseClient: fixture.manager.fetcher}
			fixture.manager.fetcher = client
			check, err := fixture.manager.Check(t.Context(), release.ChannelMain)
			if !errors.Is(err, test.want) || check.UpdateAvailable || check.SigningStatus == "VERIFIED" ||
				!containsReason(check.ReasonCodes, ReasonCode(test.want)) || len(check.ArtifactTrust) != 0 {
				t.Fatalf("unsafe legacy check: %+v %v", check, err)
			}
			if _, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("provider-denied")); !errors.Is(err, test.want) {
				t.Fatalf("unsafe prepare: %v", err)
			}
			caps := fixture.manager.Capabilities(t.Context())
			if caps.Download != "UNAVAILABLE" || caps.Prepare != "UNAVAILABLE" || caps.Activate != "UNAVAILABLE" ||
				caps.Rollback != "AVAILABLE" || !containsReason(caps.ReasonCodes, ReasonCode(test.want)) {
				t.Fatalf("unsafe capability posture: %+v", caps)
			}
			if client.calls != 0 || fixture.provider.stageCalls != 0 || fixture.provider.activateCalls != 0 {
				t.Fatal("denied provider reached release transport or host mutation")
			}
			for _, row := range []any{&model.UpdateOperation{}, &model.UpdateReleaseState{}, &model.UpdateJournal{}} {
				var count int64
				if err := fixture.db.Model(row).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("denial created durable authority: %T %d %v", row, count, err)
				}
			}
			encoded, err := json.Marshal(check)
			if err != nil || strings.Contains(string(encoded), "credential-sentinel") {
				t.Fatal("unverified provider metadata echoed")
			}
		})
	}
}

func TestLegacyProviderGuardRechecksActivationAndRetainsRollback(t *testing.T) {
	for _, id := range []string{"shtorm-extended", ""} {
		t.Run(id, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			prepared, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("before-provider-change"))
			if err != nil {
				t.Fatal(err)
			}
			fixture.manager.coreProvider = func() coreprovider.Identity { return coreprovider.Resolve(nil) }
			want := ErrCoreProviderUnverified
			if id != "" {
				fixture.manager.coreProvider = updateTestCoreProvider(t, id)
				want = ErrCoreProviderMaintenance
			}
			before, _ := json.Marshal(prepared)
			actual, err := fixture.manager.Activate(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
			if !errors.Is(err, want) || fixture.provider.activateCalls != 0 {
				t.Fatalf("stale provider activation admitted: %+v %v", actual, err)
			}
			persisted, err := fixture.manager.Operation(t.Context(), prepared.OperationID)
			after, encodeErr := json.Marshal(persisted)
			if err != nil || encodeErr != nil || !bytes.Equal(before, after) {
				t.Fatal("denied activation changed its checkpoint")
			}
			rolledBack, err := fixture.manager.Rollback(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
			if err != nil || rolledBack.State != string(StateRolledBack) || fixture.provider.rollbackCalls != 1 {
				t.Fatalf("provider guard prevented checkpoint recovery: %+v %v", rolledBack, err)
			}
		})
	}
}
