//go:build linux

package runtimecontract

import (
	"slices"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

// Only the preboot allocation option differs. These are the bounded mountinfo
// values retained from the physical beta8 clean-install failure; this test does
// not claim to recover the broker's uncaptured historical inner error.
func TestSystemdMountBindingColdBootPresentation(t *testing.T) {
	root := Installed().RuntimeRoot
	makeProof := func(options string) RuntimeMountProofV1 {
		t.Helper()
		fact, err := mountevidence.Parse([]byte("30 1 179:97 / / rw,relatime - ext4 /dev/mmcblk0p1 "+options+"\n"), root)
		if err != nil {
			t.Fatal(err)
		}
		fact, err = mountevidence.BindStatfs(fact, root, false, 0xef53)
		if err != nil {
			t.Fatal(err)
		}
		proof, err := NewRuntimeMountProof(root, RuntimeMountPersistent, fact)
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	installed := makeProof("rw,errors=remount-ro,commit=120,mb_optimize_scan=0")
	current := makeProof("rw,errors=remount-ro,commit=120")
	if slices.Equal(installed.Mount.SuperOptions, current.Mount.SuperOptions) {
		t.Fatal("negative control did not vary the captured field")
	}
	if !sameSystemdRuntimeBacking(installed, current) {
		t.Fatalf("same backing rejected: field=SuperOptions installed=%v postboot=%v", installed.Mount.SuperOptions, current.Mount.SuperOptions)
	}
}

func TestSystemdMountBindingRejectsDifferentBacking(t *testing.T) {
	installed := testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent)
	installed.Mount.MountPoint = "/"
	if err := installed.Mount.Seal(); err != nil {
		t.Fatal(err)
	}
	installed.Revision = installed.revision()
	for name, mutate := range map[string]func(*mountevidence.Fact){
		"different device":         func(f *mountevidence.Fact) { f.Device = "9:9" },
		"different source":         func(f *mountevidence.Fact) { f.Source = "/dev/other" },
		"different filesystem":     func(f *mountevidence.Fact) { f.Filesystem = "xfs" },
		"different magic":          func(f *mountevidence.Fact) { f.FilesystemMagic++ },
		"different directory":      func(f *mountevidence.Fact) { f.Root = "/other" },
		"read only":                func(f *mountevidence.Fact) { f.MountOptions = []string{"ro"} },
		"kernel read only":         func(f *mountevidence.Fact) { f.KernelReadOnly = true },
		"superblock read only":     func(f *mountevidence.Fact) { f.SuperOptions = []string{"ro"} },
		"missing superblock mode":  func(f *mountevidence.Fact) { f.SuperOptions = nil },
		"conflicting super mode":   func(f *mountevidence.Fact) { f.SuperOptions = []string{"ro", "rw"} },
		"different canonical root": func(f *mountevidence.Fact) { f.ResolvedTarget = "/other/.runtime/server-protection" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := installed
			mutate(&changed.Mount)
			if err := changed.Mount.Seal(); err != nil {
				t.Fatal(err)
			}
			changed.Revision = changed.revision()
			if sameSystemdRuntimeBacking(installed, changed) {
				t.Fatal("foreign or read-only backing accepted")
			}
		})
	}
	changed := installed
	changed.Mount.MountID++
	changed.Mount.ParentID++
	changed.Mount.MountPoint = changed.Root
	changed.Mount.Root = changed.Root
	if err := changed.Mount.Seal(); err != nil {
		t.Fatal(err)
	}
	changed.Revision = changed.revision()
	if !sameSystemdRuntimeBacking(installed, changed) {
		t.Fatal("same filesystem directory in private bind mount rejected")
	}
}

func TestSystemdMountBindingRetainsSuperblockSecurityPolicy(t *testing.T) {
	installed := testRuntimeMountProof(t, Installed().RuntimeRoot, RuntimeMountPersistent)
	for _, option := range []string{"sync", "dirsync", "mand", "lazytime", "acl", "noacl", "user_xattr", "nouser_xattr",
		"context=system_u:object_r:var_t:s0", "fscontext=a", "defcontext=a", "rootcontext=a", "seclabel",
		"smackfsdef=a", "smackfsfloor=a", "smackfshat=a", "smackfsroot=a", "smackfstransmute=a"} {
		t.Run(option, func(t *testing.T) {
			changed := installed
			changed.Mount.SuperOptions = append(slices.Clone(installed.Mount.SuperOptions), option)
			if err := changed.Mount.Seal(); err != nil {
				t.Fatal(err)
			}
			changed.Revision = changed.revision()
			if sameSystemdRuntimeBacking(installed, changed) || sameSystemdRuntimeBacking(changed, installed) {
				t.Fatal("changed superblock access policy accepted")
			}
		})
	}
}
