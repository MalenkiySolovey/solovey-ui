//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

func TestCompletePreRemoveCleansFirewallBeforeRuntimeAndFailsVisible(t *testing.T) {
	var events []string
	err := completePreRemove(t.Context(), func(context.Context) error {
		events = append(events, "firewall")
		return nil
	}, func() error {
		events = append(events, "runtime")
		return nil
	})
	if err != nil || !slices.Equal(events, []string{"firewall", "runtime"}) {
		t.Fatalf("pre-remove ordering = %v, err=%v", events, err)
	}
	sentinel := errors.New("firewall cleanup failed")
	events = nil
	err = completePreRemove(t.Context(), func(context.Context) error {
		events = append(events, "firewall")
		return sentinel
	}, func() error {
		events = append(events, "runtime")
		return nil
	})
	if !errors.Is(err, sentinel) || !slices.Equal(events, []string{"firewall"}) {
		t.Fatalf("failed firewall cleanup did not fence runtime deletion: events=%v err=%v", events, err)
	}
}

func TestVerifyProcdControlPlane(t *testing.T) {
	var program string
	var arguments []string
	err := verifyProcdControlPlane(func(name string, values ...string) error {
		program = name
		arguments = append([]string(nil), values...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if program != "/bin/ubus" || !slices.Equal(arguments, []string{"-S", "call", "service", "list", `{"name":"solovey-ui"}`}) {
		t.Fatalf("procd control-plane probe = %q %q", program, arguments)
	}
	sentinel := errors.New("ubus unavailable")
	if err := verifyProcdControlPlane(func(string, ...string) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("unavailable control plane was not reported: %v", err)
	}
}

func TestVerifyBootLinksRemoved(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		if err := verifyBootLinksRemoved(filepath.Join(t.TempDir(), "missing")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unrelated entries", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"S95another-service", "solovey-ui", "S9solovey-ui", "S950solovey-ui"} {
			if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := verifyBootLinksRemoved(root); err != nil {
			t.Fatal(err)
		}
	})
	for _, name := range []string{"S95solovey-ui", "K05solovey-ui"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyBootLinksRemoved(root); err == nil {
				t.Fatal("enabled package service was accepted")
			}
		})
	}
}

func TestCleanupPackageRuntime(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		if err := cleanupPackageRuntime(filepath.Join(t.TempDir(), "missing"), 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("nested runtime tree", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "runtime")
		durable := filepath.Join(base, "durable", "db.sqlite")
		if err := os.MkdirAll(filepath.Join(root, "server-protection", "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(durable), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "server-protection", "state"), []byte("volatile"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(durable, []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanupPackageRuntime(root, ownerUID(t, root)); err != nil {
			t.Fatal(err)
		}
		if err := cleanupPackageRuntime(root, ownerUID(t, base)); err != nil {
			t.Fatalf("repeated runtime cleanup failed: %v", err)
		}
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("runtime root remains after cleanup: %v", err)
		}
		data, err := os.ReadFile(durable)
		if err != nil || string(data) != "preserve" {
			t.Fatalf("durable sibling was changed: %q, %v", data, err)
		}
	})
	t.Run("symlink root", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		root := filepath.Join(base, "runtime")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "preserve"), []byte("durable"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, root); err != nil {
			t.Fatal(err)
		}
		if err := cleanupPackageRuntime(root, ownerUID(t, target)); err == nil {
			t.Fatal("symlink runtime root was accepted")
		}
		if _, err := os.Stat(filepath.Join(target, "preserve")); err != nil {
			t.Fatalf("symlink target was changed: %v", err)
		}
	})
	t.Run("regular file root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "runtime")
		if err := os.WriteFile(root, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanupPackageRuntime(root, ownerUID(t, root)); err == nil {
			t.Fatal("regular-file runtime root was accepted")
		}
	})
	t.Run("unexpected owner", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "runtime")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		actual := ownerUID(t, root)
		if err := cleanupPackageRuntime(root, actual+1); err == nil {
			t.Fatal("unexpected runtime owner was accepted")
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("rejected runtime root was changed: %v", err)
		}
	})
}

func ownerUID(t testing.TB, path string) uint32 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("filesystem did not expose Linux ownership")
	}
	return stat.Uid
}
