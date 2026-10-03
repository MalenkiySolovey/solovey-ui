//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

const packageExecTestMode = "SOLOVEY_PACKAGE_EXEC_TEST_MODE"

type packageExecObservation struct {
	PID             int
	OriginalPID     int
	UID             int
	GID             int
	NoNewPrivileges int
	ThreadsChecked  int
	ExecHops        int
	Environment     string
	Failure         string
}

func TestPackageExecKernelInheritanceAndIdentity(t *testing.T) {
	// A world-traversable temporary copy permits the root-run nonroot case
	// without changing the original Go test binary or any service account.
	executable := packageExecTestExecutable(t)
	cases := []struct {
		name       string
		credential *syscall.Credential
		uid, gid   int
	}{
		{name: "current_identity", uid: os.Geteuid(), gid: os.Getegid()},
	}
	if os.Geteuid() == 0 {
		cases = append(cases, struct {
			name       string
			credential *syscall.Credential
			uid, gid   int
		}{name: "nonroot_panel_identity", credential: &syscall.Credential{Uid: 32768, Gid: 32768}, uid: 32768, gid: 32768})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := runPackageExecHelper(t, executable, "harden", tc.credential)
			if observation.PID != observation.OriginalPID || observation.UID != tc.uid || observation.GID != tc.gid ||
				observation.NoNewPrivileges != 1 || observation.ThreadsChecked < 8 || observation.ExecHops != 2 ||
				observation.Environment != "openwrt-package-managed" {
				t.Fatalf("same-PID package exec/kernel inheritance: %#v", observation)
			}
			t.Logf("PID retained=%d UID/GID=%d/%d kernel NoNewPrivs=1 on %d threads after %d exec hops",
				observation.PID, observation.UID, observation.GID, observation.ThreadsChecked, observation.ExecHops)
		})
	}
	// This witness distinguishes the original plain-exec behavior from actual
	// enforcement; a host already hardened by its supervisor cannot clear NNP.
	control := runPackageExecHelper(t, executable, "plain", nil)
	if control.NoNewPrivileges == 0 {
		t.Log("negative control: plain same-PID exec retains kernel NoNewPrivs=0")
	} else {
		t.Log("negative control unavailable: host supervisor already supplied no_new_privs")
	}
}

func TestPackageExecFailsClosed(t *testing.T) {
	executable := packageExecTestExecutable(t)
	for _, mode := range []string{"deny-set", "deny-get", "zero-get", "missing-executable"} {
		t.Run(mode, func(t *testing.T) {
			observation := runPackageExecHelper(t, executable, mode, nil)
			if observation.ExecHops != 0 || observation.PID != observation.OriginalPID || observation.Failure != mode {
				t.Fatalf("failed kernel/exec operation crossed package launch boundary: %#v", observation)
			}
		})
	}
}

func packageExecTestExecutable(t *testing.T) string {
	t.Helper()
	// A caller's private TMPDIR can be non-traversable by the nonroot child.
	root, err := os.MkdirTemp("/tmp", "solovey-package-exec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "package-exec-test")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runPackageExecHelper(t *testing.T, executable, mode string, credential *syscall.Credential) packageExecObservation {
	t.Helper()
	command := exec.Command(executable, "-test.run=^TestPackageExecKernelHelper$")
	command.Env = []string{packageExecTestMode + "=" + mode, "SUI_DEPLOYMENT_KIND=openwrt-package-managed"}
	if credential != nil {
		command.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	// Coverage-instrumented subprocesses may emit runtime diagnostics on
	// stderr. Only stdout carries the observation protocol.
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("kernel helper %s: %v, %s%s", mode, err, output, diagnostics.String())
	}
	var result packageExecObservation
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("kernel helper observation: %v, %s", err, output)
	}
	return result
}

func TestPackageExecKernelHelper(t *testing.T) {
	mode := os.Getenv(packageExecTestMode)
	if mode == "" {
		return
	}
	runtime.LockOSThread()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{executable, "-test.run=^TestPackageExecKernelHelper$"}
	original := os.Getenv("SOLOVEY_PACKAGE_EXEC_ORIGINAL_PID")
	if original == "" {
		original = strconv.Itoa(os.Getpid())
	}
	originalPID, err := strconv.Atoi(original)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "harden" || mode == "plain" || mode == "second-exec" {
		next := "observe"
		if mode == "harden" {
			next = "second-exec"
		}
		hops, _ := strconv.Atoi(os.Getenv("SOLOVEY_PACKAGE_EXEC_HOPS"))
		environment := []string{packageExecTestMode + "=" + next, "SOLOVEY_PACKAGE_EXEC_ORIGINAL_PID=" + original,
			"SUI_DEPLOYMENT_KIND=" + os.Getenv("SUI_DEPLOYMENT_KIND"), "SOLOVEY_PACKAGE_EXEC_HOPS=" + strconv.Itoa(hops+1)}
		if mode == "plain" || mode == "second-exec" {
			// Like the existing readiness -> panel handoff, this second exec
			// relies on inheritance rather than setting the kernel bit again.
			err = syscall.Exec(executable, arguments, environment)
		} else {
			err = execPackageProcess(executable, arguments, environment)
		}
		t.Fatalf("package exec returned: %v", err)
	}
	if mode == "observe" {
		// Force several new OS threads in the replacement image. Every new
		// thread must inherit the hardened exec thread's kernel bit.
		var ready sync.WaitGroup
		ready.Add(8)
		for range 8 {
			go func() { runtime.LockOSThread(); ready.Done(); select {} }()
		}
		ready.Wait()
		entries, err := os.ReadDir("/proc/self/task")
		if err != nil {
			t.Fatal(err)
		}
		active, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, entry := range entries {
			status, err := os.ReadFile(filepath.Join("/proc/self/task", entry.Name(), "status"))
			if os.IsNotExist(err) {
				continue // A runtime worker exited after enumeration.
			}
			if err != nil || !strings.Contains(string(status), fmt.Sprintf("NoNewPrivs:\t%d\n", active)) {
				t.Fatalf("thread %s did not inherit kernel bit%d: %v", entry.Name(), active, err)
			}
			checked++
		}
		hops, _ := strconv.Atoi(os.Getenv("SOLOVEY_PACKAGE_EXEC_HOPS"))
		_ = json.NewEncoder(os.Stdout).Encode(packageExecObservation{PID: os.Getpid(), OriginalPID: originalPID,
			UID: os.Geteuid(), GID: os.Getegid(), NoNewPrivileges: active, ThreadsChecked: checked,
			ExecHops: hops, Environment: os.Getenv("SUI_DEPLOYMENT_KIND")})
		os.Exit(0)
	}
	if mode != "missing-executable" {
		installPackageExecDenyFilter(t, mode)
	}
	target := executable
	if mode == "missing-executable" {
		target = filepath.Join(filepath.Dir(executable), "absent-executable")
	}
	err = execPackageProcess(target, arguments, []string{packageExecTestMode + "=unexpected-target-executed"})
	switch mode {
	case "deny-set":
		if !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), "cannot set") {
			t.Fatalf("set rejection: %v", err)
		}
	case "deny-get":
		if !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), "cannot verify") {
			t.Fatalf("readback rejection: %v", err)
		}
	case "zero-get":
		if err == nil || !strings.Contains(err.Error(), "not active") {
			t.Fatalf("unproven kernel bit: %v", err)
		}
	case "missing-executable":
		if !errors.Is(err, unix.ENOENT) {
			t.Fatalf("exec rejection: %v", err)
		}
	default:
		t.Fatalf("target was executed despite failed hardening: %s", mode)
	}
	_ = json.NewEncoder(os.Stdout).Encode(packageExecObservation{PID: os.Getpid(), OriginalPID: originalPID, Failure: mode})
	os.Exit(0)
}

func installPackageExecDenyFilter(t *testing.T, mode string) {
	t.Helper()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	option, errno := uint32(unix.PR_GET_NO_NEW_PRIVS), uint32(unix.EPERM)
	if mode == "deny-set" {
		option = unix.PR_SET_NO_NEW_PRIVS
	} else if mode == "zero-get" {
		errno = 0 // Seccomp returns0 without executing GET: unusable readback.
	}
	// Child-only, current locked thread; no filter or irreversible bit in the
	// parent test runner. The production setter and getter are real syscalls.
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: unix.SYS_PRCTL},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: option},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | errno},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
		t.Fatal(err)
	}
}
