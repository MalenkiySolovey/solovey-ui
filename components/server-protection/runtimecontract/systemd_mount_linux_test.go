//go:build linux

package runtimecontract

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

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
		"different directory":      func(f *mountevidence.Fact) { f.Root = "/other" },
		"read only":                func(f *mountevidence.Fact) { f.MountOptions = []string{"ro"} },
		"kernel read only":         func(f *mountevidence.Fact) { f.KernelReadOnly = true },
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
