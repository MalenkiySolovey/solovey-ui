//go:build linux

package deployment

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInstallerIgnoresAmbientExecutionAuthority(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise the production installer authority branch")
	}
	root := deploymentModuleRoot(t)
	installer := filepath.Join(root, "install.sh")
	testRoot := t.TempDir()
	hostile := filepath.Join(testRoot, "hostile")
	if err := os.Mkdir(hostile, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(testRoot, "ambient-path-executed")
	for _, name := range []string{"uname", "curl", "sed", "grep", "head", "stat", "readlink"} {
		body := "#!/bin/sh\nprintf hostile > \"" + marker + "\"\nexit 91\n"
		if err := os.WriteFile(filepath.Join(hostile, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, ambientPath := range []string{hostile, ""} {
		command := exec.Command("/bin/bash", installer, "--dry-run", "--version", "v-runtime-contract", "--without", "all")
		command.Env = []string{
			"PATH=" + ambientPath,
			"LANG=attacker_locale",
			"LD_LIBRARY_PATH=" + hostile,
			"SOLOVEY_UI_INSTALL_DIR=" + filepath.Join(testRoot, "install"),
			"SOLOVEY_UI_SYSTEMD_SERVICE=" + filepath.Join(testRoot, "systemd", "solovey-ui.service"),
			"SOLOVEY_UI_SYSTEMD_PROFILE_ROOT=" + filepath.Join(testRoot, "profiles"),
			"SOLOVEY_UI_DEPLOYMENT_MARKER=" + filepath.Join(testRoot, "deployment-profile"),
			"SOLOVEY_UI_APPLICATION_OWNER_CONTRACT=" + filepath.Join(testRoot, "application-owner-contract.json"),
			"SOLOVEY_UI_ENV_DIR=" + filepath.Join(testRoot, "etc"),
			"SOLOVEY_UI_BACKUP_ROOT=" + filepath.Join(testRoot, "backups"),
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("installer rejected supported base tools with ambient PATH %q: %v\n%s", ambientPath, err, output)
		}
		if strings.Contains(string(output), "hostile") {
			t.Fatalf("ambient executable affected installer output: %s", output)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ambient PATH executable ran: %v", err)
	}
}

func TestNativeInstallerProductionEnvironmentIsFixed(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise the production installer authority branch")
	}
	installer := filepath.Join(deploymentModuleRoot(t), "install.sh")
	command := exec.Command("/bin/bash", "-c", `source "$1"; printf '%s|%s|%s|%s\n' "$PATH" "$LANG" "$LC_ALL" "${LD_LIBRARY_PATH-unset}"`, "contract", installer)
	command.Env = []string{"PATH=/attacker/bin", "LANG=attacker_locale", "LC_ALL=attacker_locale", "LD_LIBRARY_PATH=/attacker/lib"}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect fixed production environment: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if got, want := lines[len(lines)-1], "/usr/sbin:/usr/bin:/sbin:/bin|C|C|unset"; got != want {
		t.Fatalf("production runtime environment = %q, want %q", got, want)
	}
}

func TestNativeInstallerMissingRequiredToolIsBounded(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise the production installer authority branch")
	}
	installer := filepath.Join(deploymentModuleRoot(t), "install.sh")
	command := exec.Command("/bin/bash", "-c", `source "$1"; require_command solovey-ui-deliberately-absent-runtime-tool`, "contract", installer)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "required native runtime tool is unavailable: solovey-ui-deliberately-absent-runtime-tool") {
		t.Fatalf("missing runtime tool result err=%v output=%q", err, output)
	}
}

func TestNativeInstallerRejectsUnsafeToolObjects(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise executable ownership rejection")
	}
	root := deploymentModuleRoot(t)
	installer := filepath.Join(root, "install.sh")
	testRoot := t.TempDir()
	if err := os.Chmod(testRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTool := func(path string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	safe := filepath.Join(testRoot, "safe")
	writeTool(safe, 0o555)
	unsafeOwner := filepath.Join(testRoot, "unsafe-owner")
	writeTool(unsafeOwner, 0o555)
	if err := os.Chown(unsafeOwner, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	unsafeMode := filepath.Join(testRoot, "unsafe-mode")
	writeTool(unsafeMode, 0o575)
	if err := os.Chmod(unsafeMode, 0o575); err != nil {
		t.Fatal(err)
	}
	unsafeDirectory := filepath.Join(testRoot, "unsafe-ancestry")
	unsafeAncestry := filepath.Join(unsafeDirectory, "tool")
	writeTool(unsafeAncestry, 0o555)
	if err := os.Chmod(unsafeDirectory, 0o777); err != nil {
		t.Fatal(err)
	}

	for _, item := range []struct {
		name    string
		path    string
		wantOK  bool
		message string
	}{
		{name: "safe", path: safe, wantOK: true},
		{name: "unsafe owner", path: unsafeOwner, message: "unsafe ownership"},
		{name: "unsafe mode", path: unsafeMode, message: "unsafe mode"},
		{name: "unsafe ancestry", path: unsafeAncestry, message: "writable ancestry"},
		{name: "missing", path: filepath.Join(testRoot, "missing"), message: "outside its trusted ancestry"},
	} {
		t.Run(item.name, func(t *testing.T) {
			command := exec.Command("/bin/bash", "-c", `source "$1"; validate_runtime_tool_object sample "$2" "$3" 0`, "contract", installer, item.path, testRoot)
			output, err := command.CombinedOutput()
			if item.wantOK {
				if err != nil {
					t.Fatalf("safe executable rejected: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), item.message) {
				t.Fatalf("unsafe executable result err=%v output=%q, want %q", err, output, item.message)
			}
		})
	}
}

func deploymentModuleRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found")
		}
		directory = parent
	}
}
