package runtimecontract

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func TestInstalledRuntimeRootProjectionsBindTheSelectedDeployment(t *testing.T) {
	const instance = "00112233-4455-4677-8899-aabbccddeeff"
	source := "src-" + strings.Repeat("1", 64)
	artifact := "art-" + strings.Repeat("2", 64)
	deployment := "dep-" + strings.Repeat("3", 64)
	executableSHA := strings.Repeat("4", 64)

	systemdBinding, err := Bind(Installed(), instance, source, artifact, deployment)
	if err != nil {
		t.Fatal(err)
	}
	systemdOwner, err := deploymentidentity.NewSystemdV1(instance, source, artifact, deployment, systemdBinding.ContractRevision, systemdBinding.BindingRevision,
		"solovey-ui", "solovey-ui.service", "/etc/systemd/system/solovey-ui.service", strings.Repeat("5", 64),
		"/system.slice/solovey-ui.service", "/usr/local/solovey-ui/releases/current/solovey-ui", executableSHA, 997, 997)
	if err != nil {
		t.Fatal(err)
	}
	systemdRuntime, err := InstalledSystemdRuntimeRoot(systemdOwner, testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent))
	if err != nil {
		t.Fatal(err)
	}
	systemdExpected, err := deploymentidentity.ExpectedSystemdApplicationOwner(systemdOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstalledRuntimeRootBinding(systemdRuntime, systemdExpected); err != nil || systemdRuntime.RuntimeRoot != Installed().RuntimeRoot {
		t.Fatalf("Systemd installed runtime binding = %#v, %v", systemdRuntime, err)
	}
	systemdProjection, err := deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendSystemd, systemdExpected)
	if err != nil {
		t.Fatal(err)
	}
	systemdAuthority, err := InstalledRuntimeRootAuthority(systemdRuntime, systemdProjection)
	if err != nil || !systemdAuthority.Installed() || systemdAuthority.Path() != Installed().RuntimeRoot {
		t.Fatalf("Systemd installed runtime authority = %#v, %v", systemdAuthority, err)
	}

	openWrtBinding, err := BindOpenWrt(OpenWrtInstalled(), instance, source, artifact, deployment)
	if err != nil {
		t.Fatal(err)
	}
	openWrtOwner, err := deploymentidentity.NewProcdV1(instance, source, artifact, deployment, openWrtBinding.ContractRevision, openWrtBinding.BindingRevision,
		"solovey-ui-panel", "solovey-ui", "panel", "/usr/lib/solovey-ui/solovey-ui", executableSHA, 997, 997)
	if err != nil {
		t.Fatal(err)
	}
	openWrtRuntime, err := InstalledOpenWrtRuntimeRoot(openWrtOwner, testRuntimeMountProof(t, OpenWrtRuntimeRoot, RuntimeMountVolatile))
	if err != nil {
		t.Fatal(err)
	}
	openWrtExpected, err := deploymentidentity.ExpectedProcdApplicationOwner(openWrtOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstalledRuntimeRootBinding(openWrtRuntime, openWrtExpected); err != nil || openWrtRuntime.RuntimeRoot != OpenWrtRuntimeRoot {
		t.Fatalf("OpenWrt installed runtime binding = %#v, %v", openWrtRuntime, err)
	}
	openWrtProjection, err := deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendProcd, openWrtExpected)
	if err != nil {
		t.Fatal(err)
	}
	openWrtAuthority, err := InstalledRuntimeRootAuthority(openWrtRuntime, openWrtProjection)
	if err != nil || !openWrtAuthority.Installed() || openWrtAuthority.Path() != OpenWrtRuntimeRoot {
		t.Fatalf("OpenWrt installed runtime authority = %#v, %v", openWrtAuthority, err)
	}
	if openWrtRuntime.RuntimeRoot == systemdRuntime.RuntimeRoot {
		t.Fatal("deployment-specific runtime roots collapsed into one platform default")
	}
	if systemdAuthority.Backend() != DeploymentBackendSystemd || openWrtAuthority.Backend() != DeploymentBackendProcd ||
		systemdAuthority.ProjectionRevision() == openWrtAuthority.ProjectionRevision() {
		t.Fatalf("retained deployment projections systemd=%q/%q procd=%q/%q",
			systemdAuthority.Backend(), systemdAuthority.ProjectionRevision(), openWrtAuthority.Backend(), openWrtAuthority.ProjectionRevision())
	}

	mountChecks := 0
	if err := recheckRetainedInstalledProjection(systemdAuthority,
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return systemdProjection, nil },
		func() (InstalledRuntimeRootV1, error) { return systemdRuntime, nil },
		func() error { mountChecks++; return nil }); err != nil || mountChecks != 1 {
		t.Fatalf("stable retained projection recheck err=%v mountChecks=%d", err, mountChecks)
	}
	mountChecks = 0
	if err := recheckRetainedInstalledProjection(systemdAuthority,
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return openWrtProjection, nil },
		func() (InstalledRuntimeRootV1, error) { return systemdRuntime, nil },
		func() error { mountChecks++; return nil }); err == nil || mountChecks != 0 {
		t.Fatalf("Systemd-to-procd proof switch err=%v mountChecks=%d", err, mountChecks)
	}
	changedOwner := systemdProjection
	changedOwner.Owner.ContractRevision = strings.Repeat("9", 64)
	if err := recheckRetainedInstalledProjection(systemdAuthority,
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return changedOwner, nil },
		func() (InstalledRuntimeRootV1, error) { return systemdRuntime, nil }, func() error { return nil }); err == nil {
		t.Fatal("application-owner revision switch was not fenced")
	}
	changedRuntime := systemdRuntime
	changedRuntime.Revision = strings.Repeat("8", 64)
	if err := recheckRetainedInstalledProjection(systemdAuthority,
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return systemdProjection, nil },
		func() (InstalledRuntimeRootV1, error) { return changedRuntime, nil }, func() error { return nil }); err == nil {
		t.Fatal("runtime-root revision switch was not fenced")
	}
	mountErr := errors.New("mount changed")
	if err := recheckRetainedInstalledProjection(systemdAuthority,
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return systemdProjection, nil },
		func() (InstalledRuntimeRootV1, error) { return systemdRuntime, nil }, func() error { return mountErr }); !errors.Is(err, mountErr) {
		t.Fatalf("mount revision switch error = %v", err)
	}

	drift := openWrtExpected
	drift.DeploymentID = "dep-" + strings.Repeat("9", 64)
	if err := ValidateInstalledRuntimeRootBinding(openWrtRuntime, drift); err == nil {
		t.Fatal("runtime root bound to another deployment was accepted")
	}
}

func TestRuntimeRootSelectionPrefersBoundDeploymentFactAndFailsClosed(t *testing.T) {
	const instance = "00112233-4455-4677-8899-aabbccddeeff"
	owner := deploymentidentity.ExpectedApplicationOwnerV1{
		Schema: deploymentidentity.ExpectedApplicationOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64), InstanceID: instance,
		SourceRevision: "src-" + strings.Repeat("1", 64), ArtifactRevision: "art-" + strings.Repeat("2", 64),
		DeploymentID: "dep-" + strings.Repeat("3", 64), RuntimeRootBindingRevision: strings.Repeat("4", 64),
		ServiceIdentity: "solovey-ui", ExecutablePath: "/usr/lib/solovey-ui/solovey-ui",
		ExecutableSHA256: strings.Repeat("5", 64), ProcessUID: 997, ProcessGID: 997,
	}
	runtime, err := newInstalledRuntimeRoot(OpenWrtRuntimeRoot, strings.Repeat("6", 64), owner.RuntimeRootBindingRevision,
		owner.ContractRevision, owner.InstanceID, owner.SourceRevision, owner.ArtifactRevision, owner.DeploymentID, 0o700,
		testRuntimeMountProof(t, OpenWrtRuntimeRoot, RuntimeMountVolatile))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendProcd, owner)
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(t.TempDir(), "persistent", "db")
	selected, err := resolveRootAuthority(database,
		func() (InstalledRuntimeRootV1, error) { return runtime, nil },
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return projection, nil })
	if err != nil || selected.Path() != OpenWrtRuntimeRoot || selected.Path() == RootForDatabaseFolder(database) || !selected.Installed() {
		t.Fatalf("deployment runtime root selection = %#v, %v", selected, err)
	}

	drift := owner
	drift.DeploymentID = "dep-" + strings.Repeat("9", 64)
	if _, err := resolveRootAuthority(database,
		func() (InstalledRuntimeRootV1, error) { return runtime, nil },
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) {
			return deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendProcd, drift)
		}); err == nil {
		t.Fatal("runtime root bound to another deployment was selected")
	}
	if _, err := resolveRootAuthority(database,
		func() (InstalledRuntimeRootV1, error) {
			return InstalledRuntimeRootV1{}, ErrInstalledRuntimeRootUnavailable
		},
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return projection, nil }); err == nil {
		t.Fatal("installed owner without runtime root binding did not fail closed")
	}
	fallback, err := resolveRootAuthority(database,
		func() (InstalledRuntimeRootV1, error) {
			return InstalledRuntimeRootV1{}, ErrInstalledRuntimeRootUnavailable
		},
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) {
			return deploymentidentity.InstalledApplicationOwnerProjection{}, deploymentidentity.ErrExpectedApplicationOwnerUnavailable
		})
	if err != nil || fallback.Path() != RootForDatabaseFolder(database) || fallback.Installed() {
		t.Fatalf("development fallback = %#v, %v", fallback, err)
	}
	if _, err := resolveRootAuthority(database,
		func() (InstalledRuntimeRootV1, error) {
			return InstalledRuntimeRootV1{}, errors.New("unsafe runtime fact")
		},
		func() (deploymentidentity.InstalledApplicationOwnerProjection, error) { return projection, nil }); err == nil {
		t.Fatal("invalid installed runtime fact did not fail closed")
	}
}

func TestDockerRuntimeRootRequiresExplicitVolatileMountProof(t *testing.T) {
	proof := testRuntimeMountProof(t, DockerRuntimeRoot, RuntimeMountVolatile)
	authority, err := DockerRuntimeRootAuthority(proof)
	if err != nil || !authority.Installed() || authority.Path() != DockerRuntimeRoot {
		t.Fatalf("Docker runtime authority = %#v, %v", authority, err)
	}
	persistent := testRuntimeMountProof(t, DockerRuntimeRoot, RuntimeMountPersistent)
	if _, err := DockerRuntimeRootAuthority(persistent); err == nil {
		t.Fatal("persistent Docker runtime mount was accepted")
	}
	overlay := testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent)
	overlay.Mount.Filesystem = "overlay"
	overlay.Revision = overlay.revision()
	if err := overlay.Validate(); err == nil {
		t.Fatal("persistent overlay runtime mount was accepted without backing identity")
	}
}

func TestPersistentRuntimeFilesystemPolicyMatchesOpenWrtDurabilityPolicy(t *testing.T) {
	for _, filesystem := range []string{"ext2", "ext3", "ext4", "f2fs", "ubifs", "jffs2", "btrfs", "xfs"} {
		proof := testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent)
		proof.Mount.Filesystem = filesystem
		if err := proof.Mount.Seal(); err != nil {
			t.Fatal(err)
		}
		proof.Revision = proof.revision()
		if err := proof.Validate(); err != nil {
			t.Errorf("approved filesystem %q rejected: %v", filesystem, err)
		}
	}
	for _, filesystem := range []string{"ntfs", "ntfs3", "tmpfs", "ramfs"} {
		proof := testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent)
		proof.Mount.Filesystem = filesystem
		if err := proof.Mount.Seal(); err != nil {
			t.Fatal(err)
		}
		proof.Revision = proof.revision()
		if err := proof.Validate(); err == nil {
			t.Errorf("unapproved filesystem %q accepted", filesystem)
		}
	}
}

func testRuntimeMountProof(t testing.TB, root, policy string) RuntimeMountProofV1 {
	t.Helper()
	filesystem, source, device, magic := "ext4", "/dev/test", "8:1", int64(0xEF53)
	if policy == RuntimeMountVolatile {
		filesystem, source, device, magic = "tmpfs", "tmpfs", "0:41", 0x01021994
	}
	fact, err := mountevidence.Parse([]byte(fmt.Sprintf("41 36 %s / %s rw - %s %s rw\n", device, root, filesystem, source)), root)
	if err == nil {
		fact, err = mountevidence.BindStatfs(fact, root, false, magic)
	}
	if err != nil {
		t.Fatal(err)
	}
	proof, err := NewRuntimeMountProof(root, policy, fact)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
