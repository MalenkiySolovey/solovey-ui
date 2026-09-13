package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func TestOpenWrtPackageManagedProviderReportsTruthWithoutSystemdAuthority(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	proof := openWrtOwnerProof(t)
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(proof)
	if err != nil {
		t.Fatal(err)
	}
	provider := &OpenWrtPackageManagedProvider{now: func() time.Time { return now },
		loadOwner:   func() (deploymentidentity.ApplicationOwnerContractProcdV1, error) { return proof, nil },
		brokerProbe: func(context.Context) (string, bool) { return strings.Repeat("b", 64), true }}
	posture, err := provider.Observe(context.Background())
	if err != nil || posture.Runtime != domain.RuntimePackageManaged || posture.Profile != domain.PackageManagedOpenWrt || posture.Systemd != nil ||
		posture.PanelUID != expected.ProcessUID || posture.PanelGID != expected.ProcessGID || posture.ValidateProjection(now) != nil {
		t.Fatalf("package-managed posture = %#v, %v", posture, err)
	}
	capabilities := provider.Capabilities(context.Background())
	if capabilities.Observe != domain.Available || capabilities.Doctor != domain.Available || capabilities.Migrate != domain.Unavailable || capabilities.Rollback != domain.Unavailable || capabilities.Validate() != nil {
		t.Fatalf("package-managed capabilities = %#v", capabilities)
	}
	presentation := provider.BrokerPresentation(context.Background())
	if !presentation.Available || strings.Contains(strings.ToLower(presentation.Transport), "systemd") || strings.Contains(strings.ToLower(presentation.PeerPosture), "systemd") {
		t.Fatalf("package-managed broker presentation = %#v", presentation)
	}
	if _, err := provider.Prepare(context.Background(), FenceV1{}, domain.NativeHardened); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("package-managed mutation error = %v", err)
	}
	if err := provider.Apply(context.Background(), FenceV1{}, domain.NativeHardened, strings.Repeat("a", 64)); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("package-managed apply error = %v", err)
	}
	if _, err := provider.Verify(context.Background(), FenceV1{}, domain.NativeHardened, strings.Repeat("a", 64)); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("package-managed verify error = %v", err)
	}
	if _, err := provider.Rollback(context.Background(), FenceV1{}, domain.NativeLegacyRoot, strings.Repeat("a", 64)); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("package-managed rollback error = %v", err)
	}
	if _, ok := any(provider).(CheckpointLifecycle); ok {
		t.Fatal("package-managed provider exposed native checkpoint recovery/release authority")
	}
	provider.brokerProbe = func(context.Context) (string, bool) { return "", false }
	posture, err = provider.Observe(context.Background())
	if err != nil || posture.BrokerAvailable || !containsReason(posture.Reasons, "privileged_broker_unavailable") || provider.BrokerPresentation(context.Background()).Available {
		t.Fatalf("package-managed unavailable broker truth = %#v, %v", posture, err)
	}
}

func TestRuntimeProviderSelectsPackageManagedBeforeNativeCompatibility(t *testing.T) {
	t.Setenv("SUI_DEPLOYMENT_KIND", OpenWrtPackageManagedDeploymentKind)
	selected, ok := RuntimeProvider().(*OpenWrtPackageManagedProvider)
	if !ok {
		t.Fatalf("package-managed deployment selected %T", RuntimeProvider())
	}
	selected.loadOwner = func() (deploymentidentity.ApplicationOwnerContractProcdV1, error) {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, errors.New("missing OpenWrt proof")
	}
	if _, err := selected.Observe(context.Background()); err == nil {
		t.Fatal("explicit OpenWrt provider selection bypassed its proof requirement")
	}
	t.Setenv("SUI_DEPLOYMENT_KIND", "unknown-kind")
	if _, ok := RuntimeProvider().(UnavailableProvider); !ok {
		t.Fatalf("unknown deployment kind selected %T", RuntimeProvider())
	}
	t.Setenv("SUI_DEPLOYMENT_KIND", "native")
	if _, ok := RuntimeProvider().(*SystemdBrokerProvider); !ok {
		t.Fatalf("explicit native deployment selected %T", RuntimeProvider())
	}
}

func openWrtOwnerProof(t testing.TB) deploymentidentity.ApplicationOwnerContractProcdV1 {
	t.Helper()
	proof, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64),
		"art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64), strings.Repeat("6", 64), strings.Repeat("4", 64),
		"solovey-ui-panel", "solovey-ui", "panel", "/usr/lib/solovey-ui/solovey-ui", strings.Repeat("5", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func TestRuntimeProviderHasNoNonDockerSystemdFallback(t *testing.T) {
	t.Setenv("SUI_DEPLOYMENT_KIND", "")
	if _, ok := RuntimeProvider().(UnavailableProvider); !ok {
		t.Fatalf("missing deployment kind selected %T", RuntimeProvider())
	}
	t.Setenv("SUI_DEPLOYMENT_KIND", " docker ")
	if _, ok := RuntimeProvider().(*DockerProvider); !ok {
		t.Fatalf("explicit Docker deployment selected %T", RuntimeProvider())
	}
	t.Setenv("SUI_DEPLOYMENT_KIND", "not-registered")
	if _, ok := RuntimeProvider().(UnavailableProvider); !ok {
		t.Fatalf("unknown deployment kind selected %T", RuntimeProvider())
	}
}
