package fronting

import (
	"context"
	"errors"
	"testing"
	"time"

	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helpercontract"
)

type unexpectedInspectionHelper struct{}

func (unexpectedInspectionHelper) Execute(context.Context, helper.Request) (helper.Response, error) {
	panic("missing inspection owner reached helper")
}

func TestMissingInspectionOwnerDeniesBeforeHelper(t *testing.T) {
	workflow := &Workflow{Helper: unexpectedInspectionHelper{}}
	identity, err := ObserveManagedRuntimeIdentityV2(context.Background(), workflow, nil, v2Revision("management"), time.Now())
	if err == nil || identity.CanonicalRuntimeIdentityRevision != "" {
		t.Fatalf("missing inspection owner admitted identity: %#v %v", identity, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ObserveManagedRuntimeIdentityV2(ctx, workflow, nil, v2Revision("management"), time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestMissingPreviewInspectionOwnerIsInactive(t *testing.T) {
	report := NewNginxAdapter().Capability(context.Background())
	if report.Supported || report.State != StateUnsupported || report.Validate != AvailabilityUnsupported || report.Activate != AvailabilityUnsupported || report.Reload != AvailabilityUnsupported {
		t.Fatalf("missing owner gained capability: %#v", report)
	}
}
