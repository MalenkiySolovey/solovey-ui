//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAtomicRootFileKeepsContractModeUnderRestrictiveUmask(t *testing.T) {
	if os.Geteuid() != 0 {
		// Use the package's existing user-namespace fixture policy on ordinary
		// Linux CI runners; root ownership is confined to disposable files.
		command := exec.CommandContext(t.Context(), "unshare", "-Ur", "--", os.Args[0], "-test.run=^TestAtomicRootFileKeepsContractModeUnderRestrictiveUmask$", "-test.v")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("root writer fixture: %v\n%s", err, output)
		}
		return
	}
	// Umask is process-global; this package uses no parallel tests. Restore
	// the original mask before leaving each case, including failed assertions.
	for _, mask := range []int{0o022, 0o027, 0o077} {
		for _, mode := range []os.FileMode{0o444, 0o400} {
			t.Run(mode.String()+"/"+os.FileMode(mask).String(), func(t *testing.T) {
				directory := t.TempDir()
				previous := syscall.Umask(mask)
				defer syscall.Umask(previous)
				name := filepath.Join(directory, "contract.json")
				data := []byte("nonsecret-root-contract\n")
				for range 2 {
					if err := atomicRootFile(name, data, mode); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(name)
					if err != nil {
						t.Fatal(err)
					}
					if info.Mode().Perm() != mode {
						t.Fatalf("published contract mode=%o want=%o under mask=%o", info.Mode().Perm(), mode, mask)
					}
					if _, err = boundedRootFile(name, 128, mode); err != nil {
						t.Fatal("exact-mode installed reader", err)
					}
					if _, err = os.Lstat(name + ".incoming"); !os.IsNotExist(err) {
						t.Fatal("temporary publication residue", err)
					}
				}
			})
		}
	}
}
