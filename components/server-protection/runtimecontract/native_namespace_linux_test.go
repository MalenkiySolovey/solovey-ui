//go:build linux

package runtimecontract

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Explicit opt-in: this test executes real systemd mount isolation on a Linux
// host, using only a uniquely named fixture. It does not touch an installation.
func TestNativeRuntimeMountNamespace(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_NATIVE_MOUNT") == "" {
		t.Skip("explicit real-systemd contract executor required")
	}
	if file := os.Getenv("SOLOVEY_TEST_MOUNT_PROOF"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var proof RuntimeMountProofV1
		if err := json.Unmarshal(data, &proof); err != nil {
			t.Fatal(err)
		}
		// Host-sealed identity must actually differ in this sandbox; otherwise
		// this executor has not reproduced the original registration failure.
		if err := recheckRuntimeMount(proof); err == nil {
			t.Fatal("sandbox did not change the mount identity")
		}
		local, err := bindSystemdRuntimeMountProof(proof)
		if err != nil {
			t.Fatal(err)
		}
		if err := recheckRuntimeMount(local); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Fatal("root executor required")
	}
	dir, err := os.MkdirTemp("/var/lib", "solovey-mount-proof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	root := filepath.Join(dir, ".runtime", "server-protection")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	proof, err := ObserveRuntimeMount(root, RuntimeMountPersistent)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh service namespace must also bind an authority whose install-boot
	// filesystem tuning presentation is no longer emitted by the current boot.
	proof.Mount.SuperOptions = append(proof.Mount.SuperOptions, "mb_optimize_scan=0")
	if err := proof.Mount.Seal(); err != nil {
		t.Fatal(err)
	}
	proof, err = NewRuntimeMountProof(root, RuntimeMountPersistent, proof.Mount)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "proof.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, bytes, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("systemd-run", "--quiet", "--wait", "--pipe", "--collect",
		"--property=ProtectSystem=strict", "--property=PrivateTmp=true", "--property=PrivateDevices=true",
		"--property=ProtectHome=true", "--property=ReadWritePaths="+root,
		"--setenv=SOLOVEY_TEST_NATIVE_MOUNT=1", "--setenv=SOLOVEY_TEST_MOUNT_PROOF="+file,
		probe, "-test.run=^TestNativeRuntimeMountNamespace$", "-test.v")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native namespace: %v\n%s", err, output)
	}
}
