//go:build linux

package serverprotection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	protectiondeployment "github.com/MalenkiySolovey/solovey-ui/components/server-protection/deploymentadapter"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	deploymentdomain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	servicedeployment "github.com/MalenkiySolovey/solovey-ui/service/deployment"
)

// TestDockerComponentStartUsesInjectedRuntimeRoot is an opt-in real-container
// host test. It is intentionally skipped outside the shipped Docker profile;
// the caller supplies the actual read-only root, UID, bind mounts, and tmpfs.
func TestDockerComponentStartUsesInjectedRuntimeRoot(t *testing.T) {
	if os.Getenv("SUI_DEPLOYMENT_KIND") != "docker" {
		t.Skip("Docker host-test requires SUI_DEPLOYMENT_KIND=docker")
	}
	if os.Getenv("SUI_SERVER_PROTECTION_RUNTIME_ROOT") != protectionruntime.DockerRuntimeRoot {
		t.Fatal("Docker runtime-root injection is absent or non-canonical")
	}
	profileID := deploymentdomain.ProfileID(os.Getenv("SOLOVEY_DEPLOYMENT_PROFILE"))
	profile, ok := deploymentdomain.Lookup(profileID)
	if !ok || profile.Runtime != deploymentdomain.RuntimeDocker {
		t.Fatalf("Docker profile = %q, want a current shipped Docker profile", profileID)
	}
	posture, err := servicedeployment.NewDockerProvider().Observe(context.Background())
	if err != nil {
		t.Fatalf("observe effective shipped Docker runtime: %v", err)
	}
	wantReasons := []string{"docker_network_mode_unattested", "docker_engine_mode_unattested"}
	wantCapabilityMask := uint64(0)
	if profileID == deploymentdomain.DockerNetworkAdvanced {
		// Bits 10, 12, and 13 are the three capabilities bounded by the
		// shipped advanced profile. A non-root test executable can retain a
		// smaller effective set, but it may never receive an extra bit.
		wantCapabilityMask = 1<<10 | 1<<12 | 1<<13
	}
	effectiveCapabilities, boundedCapabilities := dockerCapabilityMasks(t)
	if boundedCapabilities != wantCapabilityMask || effectiveCapabilities&^wantCapabilityMask != 0 {
		t.Fatalf("Docker capability masks effective=%#x bounded=%#x want bounded=%#x", effectiveCapabilities, boundedCapabilities, wantCapabilityMask)
	}
	if profileID == deploymentdomain.DockerNetworkAdvanced && effectiveCapabilities != wantCapabilityMask {
		wantReasons = append([]string{"docker_capability_set_mismatch"}, wantReasons...)
	}
	if posture.Profile != profileID || posture.InstalledProfile != profileID || posture.ActiveProfile != profileID ||
		posture.VerifiedProfile != "" || posture.Runtime != deploymentdomain.RuntimeDocker || posture.PanelUID != 65532 ||
		posture.PanelGID != 65532 || posture.PanelRoot || posture.BrokerAvailable || !slices.Equal(posture.Reasons, wantReasons) {
		t.Fatalf("effective shipped Docker posture = %#v", posture)
	}
	t.Logf("shipped Docker profile=%s effectiveCapabilities=%#x boundedCapabilities=%#x", profileID, effectiveCapabilities, boundedCapabilities)
	if err := posture.ValidateProjection(time.Now().UTC()); err != nil {
		t.Fatalf("effective Docker posture projection: %v", err)
	}
	if err := os.WriteFile("/.solovey-ui-root-write-probe", []byte("must fail\n"), 0o600); err == nil {
		_ = os.Remove("/.solovey-ui-root-write-probe")
		t.Fatal("Docker root filesystem is writable")
	}
	for _, writable := range []string{"/data", "/cert"} {
		probe := filepath.Join(writable, ".solovey-ui-write-probe")
		if err := os.WriteFile(probe, []byte("ok\n"), 0o600); err != nil {
			t.Fatalf("Docker write scope %s is unavailable: %v", writable, err)
		}
		if err := os.Remove(probe); err != nil {
			t.Fatal(err)
		}
	}
	root := protectionruntime.DockerRuntimeRoot
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		t.Fatalf("Docker runtime root = %v, %v", info, err)
	}
	probe := filepath.Join(root, ".solovey-ui-runtime-write-probe")
	if err := os.WriteFile(probe, []byte("ok\n"), 0o600); err != nil {
		t.Fatalf("Docker runtime root is not writable by the service identity: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	authority, err := protectionruntime.ResolveRootAuthority("/data")
	if err != nil || !authority.Installed() || authority.Path() != root {
		t.Fatalf("Docker runtime authority = %#v, %v", authority, err)
	}
	projection, err := protectiondeployment.RecoveryProjectionForRuntimeAuthority(authority)
	if err != nil || projection.Authority != protectiondeployment.RecoveryAuthorityExternalOperator || projection.SelfRecoveryAvailable || projection.Action.Program != "operator_review" {
		t.Fatalf("Docker recovery authority = %#v, %v", projection, err)
	}

	if err := os.MkdirAll("/data", 0o700); err != nil {
		t.Fatal(err)
	}
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join("/data", "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	c := component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	lifecycleCtx := lifecycle.Context{Host: componenthost.Deps{API: componenthost.APIDeps{Runtime: coreservice.NewRuntime(nil)}}}
	if err := c.Start(context.Background(), lifecycleCtx); err != nil {
		t.Fatalf("Docker component Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	hooks.Lock()
	storage := hooks.artifactStorage
	hooks.Unlock()
	if storage == nil || storage.Root() != root {
		t.Fatalf("Docker artifact storage root = %#v, want %q", storage, root)
	}
	if _, err := os.Stat(filepath.Join("/data", ".runtime", "server-protection")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Docker component derived an out-of-mount runtime root: %v", err)
	}
}

func dockerCapabilityMasks(t *testing.T) (uint64, uint64) {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "CapEff:" && fields[0] != "CapBnd:" {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 16, 64)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", fields[0], parseErr)
		}
		values[fields[0]] = value
	}
	effective, effectiveOK := values["CapEff:"]
	bounded, boundedOK := values["CapBnd:"]
	if !effectiveOK || !boundedOK {
		t.Fatalf("Docker capability facts are unavailable: %v", values)
	}
	return effective, bounded
}
