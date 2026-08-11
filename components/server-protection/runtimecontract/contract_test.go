package runtimecontract

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledContractIsCanonicalAndVersioned(t *testing.T) {
	contract := Installed()
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	if contract.RuntimeRoot != "/usr/local/solovey-ui/.runtime/server-protection" ||
		contract.ArtifactRoot != contract.RuntimeRoot || contract.HelperManagedRoot != contract.RuntimeRoot ||
		contract.OperationsRoot != contract.RuntimeRoot+"/operations" || contract.DirectoryMode != 0o700 {
		t.Fatalf("installed runtime contract is incoherent: %#v", contract)
	}
	revision, err := contract.Revision()
	if err != nil || len(revision) != 64 {
		t.Fatalf("contract revision = %q, %v", revision, err)
	}
}

func TestBindingIncludesExactDeploymentIdentity(t *testing.T) {
	binding, err := Bind(Installed(), "00000000-0000-4000-8000-000000000000", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64))
	if err != nil || binding.Validate() != nil {
		t.Fatalf("valid binding rejected: %#v %v", binding, err)
	}
	changed := binding
	changed.DeploymentID = "dep-" + strings.Repeat("4", 64)
	if changed.Validate() == nil {
		t.Fatal("stale deployment binding was accepted")
	}
	for name, mutate := range map[string]func(*Binding){
		"instance": func(value *Binding) { value.InstanceID = "10000000-0000-4000-8000-000000000000" },
		"source":   func(value *Binding) { value.SourceRevision = "src-" + strings.Repeat("4", 64) },
		"artifact": func(value *Binding) { value.ArtifactRevision = "art-" + strings.Repeat("4", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := binding
			mutate(&changed)
			if changed.Validate() == nil {
				t.Fatalf("stale %s journal/deployment binding was accepted", name)
			}
		})
	}
}

func TestContractRejectsAlternateOrUnsafeRoots(t *testing.T) {
	mutations := []func(*RuntimeRootContract){
		func(c *RuntimeRootContract) { c.RuntimeRoot = DeprecatedRoot },
		func(c *RuntimeRootContract) { c.ArtifactRoot += "/other" },
		func(c *RuntimeRootContract) { c.OperationsRoot += "/other" },
		func(c *RuntimeRootContract) { c.HelperManagedRoot = DeprecatedRoot },
		func(c *RuntimeRootContract) { c.OwnerIdentity = "root" },
		func(c *RuntimeRootContract) { c.MutationAuthority = "panel" },
		func(c *RuntimeRootContract) { c.DirectoryMode = 0o755 },
		func(c *RuntimeRootContract) { c.SymlinkPolicy = "allow" },
		func(c *RuntimeRootContract) { c.MountPolicy = "allow" },
		func(c *RuntimeRootContract) { c.DeprecatedRoots = append(c.DeprecatedRoots, c.RuntimeRoot) },
	}
	for index, mutate := range mutations {
		contract := Installed()
		mutate(&contract)
		if contract.Validate() == nil {
			t.Fatalf("unsafe contract mutation %d was accepted: %#v", index, contract)
		}
	}
}

func TestDatabaseRelativeProducerUsesTheSharedRootShape(t *testing.T) {
	database := filepath.Join(t.TempDir(), "db")
	want := filepath.Join(filepath.Dir(database), ".runtime", "server-protection")
	if got := RootForDatabaseFolder(database); got != want {
		t.Fatalf("derived runtime root = %q, want %q", got, want)
	}
}

func TestNativeHardenedDatabaseUsesInstalledRuntimeContract(t *testing.T) {
	if got := RootForDatabaseFolder(path.Join(NativeDataRoot, "db")); got != Installed().RuntimeRoot {
		t.Fatalf("native hardened runtime root = %q, want %q", got, Installed().RuntimeRoot)
	}
}
