package resourceinventory

import (
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

func TestRegisterCoreContributorsRollsBackPartialRegistration(t *testing.T) {
	control := coreinboundcontrol.New(nil, nil)
	unregisterConflict, err := hostresources.RegisterInboundTransportCapabilityProviderV2(NewCoreInboundTransportProviderV2(nil, control))
	if err != nil {
		t.Fatalf("register conflict fixture: %v", err)
	}
	t.Cleanup(unregisterConflict)

	if stop, err := RegisterCoreContributors(nil, nil, control); err == nil {
		if stop != nil {
			stop()
		}
		t.Fatal("core contributor registration ignored duplicate transport authority")
	}
	if _, exists := hostresources.DefaultFrontingBackendsV1.EndpointLeaseProviderV1(coreFrontingBackendProviderIDV1); exists {
		t.Fatal("failed registration leaked the earlier fronting provider")
	}
}

func TestRegisterCoreContributorsRejectsMissingControl(t *testing.T) {
	if stop, err := RegisterCoreContributors(nil, nil, nil); err == nil {
		if stop != nil {
			stop()
		}
		t.Fatal("core contributor registration accepted missing inbound control")
	}
}
