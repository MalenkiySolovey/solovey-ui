// Package serverprotection provides test-only composition fixtures without
// widening the production privileged protocol.
package serverprotection

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func OpenWrtRuntimeAuthority(t testing.TB) protectionruntime.RuntimeRootAuthority {
	t.Helper()
	const instance = "00112233-4455-4677-8899-aabbccddeeff"
	source := "src-" + strings.Repeat("1", 64)
	artifact := "art-" + strings.Repeat("2", 64)
	deployment := "dep-" + strings.Repeat("3", 64)
	binding, err := protectionruntime.BindOpenWrt(protectionruntime.OpenWrtInstalled(), instance, source, artifact, deployment)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := deploymentidentity.NewProcdV1(instance, source, artifact, deployment, binding.ContractRevision, binding.BindingRevision,
		"solovey-ui-panel", "solovey-ui", "panel", "/usr/lib/solovey-ui/solovey-ui", strings.Repeat("4", 64), 997, 997)
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot, err := protectionruntime.InstalledOpenWrtRuntimeRoot(owner, RuntimeMountProof(t, protectionruntime.OpenWrtRuntimeRoot, protectionruntime.RuntimeMountVolatile))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(owner)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendProcd, expected)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := protectionruntime.InstalledRuntimeRootAuthority(runtimeRoot, projection)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func RuntimeMountProof(t testing.TB, root, policy string) protectionruntime.RuntimeMountProofV1 {
	t.Helper()
	filesystem, source, device, magic := "ext4", "/dev/test", "8:1", int64(0xEF53)
	if policy == protectionruntime.RuntimeMountVolatile {
		filesystem, source, device, magic = "tmpfs", "tmpfs", "0:41", 0x01021994
	}
	line := fmt.Sprintf("41 36 %s / %s rw,nosuid,nodev - %s %s rw,size=16384k\n", device, root, filesystem, source)
	fact, err := mountevidence.Parse([]byte(line), root)
	if err == nil {
		fact, err = mountevidence.BindStatfs(fact, root, false, magic)
	}
	if err != nil {
		t.Fatal(err)
	}
	proof, err := protectionruntime.NewRuntimeMountProof(root, policy, fact)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func ManagedRoot(t testing.TB, rootPath string) protectionhelper.ManagedRoot {
	t.Helper()
	base := filepath.Dir(filepath.Dir(filepath.Clean(rootPath)))
	authority, err := protectionruntime.RootAuthorityForDatabaseFolder(filepath.Join(base, "db"))
	if err != nil {
		t.Fatal(err)
	}
	if authority.Path() != filepath.Clean(rootPath) {
		t.Fatalf("test runtime authority = %q, root = %q", authority.Path(), rootPath)
	}
	root, err := protectionhelper.NewManagedRoot(authority)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func SystemdRecoveryProjection() protectionartifacts.RecoveryProjection {
	return protectionartifacts.RecoveryProjection{Authority: "authenticated_broker", SelfRecoveryAvailable: true, Action: protectionartifacts.RecoveryAction{
		Program: "systemctl", Args: []string{"is-active", "solovey-privileged-broker.socket"}, Purpose: "verify_privileged_broker_socket",
	}}
}
