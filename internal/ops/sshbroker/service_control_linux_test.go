//go:build linux

package sshbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

func TestProcdUbusCandidateSetIncludesOfficialOpenWrt25Path(t *testing.T) {
	want := []string{"/bin/ubus", "/sbin/ubus"}
	if got := procdUbusCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("procd ubus candidates = %v, want %v", got, want)
	}
}

func TestFixedResolverAcceptsOpenWrt25BinUbusLayout(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "ubus")
	missingSbin := filepath.Join(root, "sbin", "ubus")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := firstFixedBinaryWithPolicy(executableobject.Policy{RequireRegular: true, RequireExecutable: true, ForbiddenMode: 0o022}, bin, missingSbin)
	if err != nil || got.Label() != bin {
		t.Fatalf("official OpenWrt ubus layout resolved to %q, %v", got.Label(), err)
	}
	defer got.Close()
}

func TestFixedBinaryFailureHasSafeOwnerCapabilityDiagnostic(t *testing.T) {
	err := sshStartupFailure("service.supervision", "procd", errRequiredFixedHostBinaryUnavailable)
	if got, want := err.Error(), "owner=ssh capability=service.supervision adapter=procd reason=required_executable_unavailable"; got != want {
		t.Fatalf("fixed-binary diagnostic = %q, want %q", got, want)
	}
}

func TestSSHStartupFailurePreservesOpenWrtSemanticReasons(t *testing.T) {
	tests := []struct {
		name, reason string
		cause        error
	}{
		{name: "init script", reason: "init_script_absent", cause: errOpenWrtInitScriptAbsent},
		{name: "ubus", reason: "ubus_unavailable", cause: errUbusUnavailable},
		{name: "dropbear", reason: "dropbear_binary_absent", cause: errDropbearBinaryUnavailable},
		{name: "procd", reason: "procd_unavailable", cause: errProcdUnavailable},
		{name: "service absent", reason: "service_not_registered", cause: errServiceNotRegistered},
		{name: "service stopped", reason: "service_registered_stopped", cause: errServiceRegisteredStopped},
		{name: "control rejected", reason: "control_operation_rejected", cause: errControlOperationRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := sshStartupFailure("service.supervision", "procd", test.cause)
			if got := err.Error(); !strings.Contains(got, "reason="+test.reason) {
				t.Fatalf("diagnostic=%q, want reason %q", got, test.reason)
			}
		})
	}
}

func TestClassifyProcdServiceStates(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		state procdServiceState
		err   error
	}{
		{name: "not registered", raw: `{}`, state: procdServiceNotRegistered},
		{name: "registered stopped", raw: `{"dropbear":{"instances":{}}}`, state: procdServiceRegisteredStopped},
		{name: "running", raw: `{"dropbear":{"instances":{"instance":{"running":true,"pid":42}}}}`, state: procdServiceRunning},
		{name: "wrong service", raw: `{"other":{"instances":{}}}`, err: errControlOperationRejected},
		{name: "multiple services", raw: `{"dropbear":{"instances":{}},"other":{"instances":{}}}`, err: errControlOperationRejected},
		{name: "missing instances", raw: `{"dropbear":{}}`, err: errControlOperationRejected},
		{name: "malformed", raw: `{`, err: errControlOperationRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := classifyProcdService([]byte(test.raw), "dropbear")
			if test.err != nil {
				if err == nil {
					t.Fatalf("malformed projection error=%v", err)
				}
				return
			}
			if err != nil || state != test.state {
				t.Fatalf("state=%q err=%v, want %q", state, err, test.state)
			}
		})
	}
}

func TestOpenWrtInitScriptMissingIsDistinct(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dropbear")
	_, err := openOpenWrtInitScriptAt(path)
	if !errors.Is(err, errOpenWrtInitScriptAbsent) {
		t.Fatalf("missing init script error=%v, want errOpenWrtInitScriptAbsent", err)
	}
}

func TestOpenWrtInitScriptIgnoresPlatformAncestorMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux platform fixture is required")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o775); err != nil {
		t.Fatal(err)
	}
	initDir := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(initDir, "dropbear")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	object, err := openOpenWrtInitScriptAt(path)
	if err != nil {
		t.Fatalf("platform-owned init script rejected by irrelevant ancestor mode: %v", err)
	}
	defer object.Close()
}

func TestOpenWrtInitScriptRejectsWritableObjectAndDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux platform fixture is required")
	}
	root := t.TempDir()
	initDir := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(initDir, "dropbear")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o775); err != nil {
		t.Fatal(err)
	}
	if object, err := openOpenWrtInitScriptAt(path); !errors.Is(err, errOpenWrtInitScriptRejected) || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatalf("writable script was accepted: object=%v err=%v", object, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(initDir, 0o775); err != nil {
		t.Fatal(err)
	}
	if object, err := openOpenWrtInitScriptAt(path); !errors.Is(err, errOpenWrtInitScriptRejected) || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatalf("writable init.d was accepted: object=%v err=%v", object, err)
	}
}

func TestOpenWrtInitScriptRejectsSymlink(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux platform fixture is required")
	}
	root := t.TempDir()
	initDir := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(initDir, "target")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(initDir, "dropbear")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if object, err := openOpenWrtInitScriptAt(path); !errors.Is(err, errOpenWrtInitScriptRejected) || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatalf("init-script symlink was accepted: object=%v err=%v", object, err)
	}
}

func TestOpenWrtInitScriptExecutionRemainsBoundAfterPathSubstitution(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux platform fixture is required")
	}
	root := t.TempDir()
	initDir := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(initDir, "dropbear")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf trusted\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	object, err := openOpenWrtInitScriptAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	if err := os.Rename(path, filepath.Join(initDir, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf substituted\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := runSSHCapabilityCommand(context.Background(), object)
	if err != nil || string(output) != "trusted" {
		t.Fatalf("descriptor-bound execution output=%q err=%v", output, err)
	}
}
