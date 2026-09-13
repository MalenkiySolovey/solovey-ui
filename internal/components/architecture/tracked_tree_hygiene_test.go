package architecture

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTrackedTreeExcludesLocalArtifacts(t *testing.T) {
	root := moduleRoot(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("tracked-tree check requires Git")
	}
	checkout, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(checkout)) != "true" {
		t.Skip("tracked-tree check requires a Git checkout")
	}
	command := exec.Command("git", "-C", root, "ls-files", "-z")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list tracked paths: %v", err)
	}

	var violations []string
	for _, item := range bytes.Split(output, []byte{0}) {
		if len(item) == 0 {
			continue
		}
		path := filepath.ToSlash(string(item))
		if reason := localArtifactReason(path); reason != "" {
			violations = append(violations, fmt.Sprintf("%s (%s)", path, reason))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("local artifacts are tracked:\n%s", strings.Join(violations, "\n"))
	}
}

func localArtifactReason(path string) string {
	for prefix, reason := range map[string]string{
		".artifacts/":                 "generated audit output",
		".claude/":                    "local tool state",
		".codex/":                     "local tool state",
		".gotmp/":                     "temporary Go workspace",
		".runtime/":                   "local runtime state",
		".tmp/":                       "temporary workspace",
		"coverage/":                   "generated coverage output",
		"dist/":                       "generated build output",
		"frontend/coverage/":          "generated coverage output",
		"frontend/dist/":              "generated frontend build",
		"frontend/node_modules/":      "installed dependencies",
		"frontend/playwright-report/": "generated browser-test report",
		"frontend/test-results/":      "generated browser-test output",
		"node_modules/":               "installed dependencies",
		"playwright-report/":          "generated browser-test report",
		"test-results/":               "generated browser-test output",
	} {
		if strings.HasPrefix(path, prefix) {
			return reason
		}
	}

	base := strings.ToLower(filepath.Base(path))
	switch base {
	case ".ds_store", "desktop.ini", "thumbs.db":
		return "operating-system metadata"
	case ".env", ".env.local", "initial-admin.txt":
		return "local configuration or secret state"
	case "coverage.out":
		return "generated coverage output"
	case "goal-objective.md":
		return "workspace goal attachment"
	}
	if strings.HasSuffix(base, ".test") {
		return "compiled Go test binary"
	}
	return ""
}
