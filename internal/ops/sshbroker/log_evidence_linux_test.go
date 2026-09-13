//go:build linux

package sshbroker

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestOpenWrtLogreadResolverInspectsRealFilesystemFixture(t *testing.T) {
	root := t.TempDir()
	public := filepath.Join(root, "sbin", "logread")
	ubox := filepath.Join(root, "usr", "libexec", "logread-ubox")
	if err := os.MkdirAll(filepath.Dir(public), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ubox), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ubox, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "usr", "libexec", "logread-ubox"), public); err != nil {
		t.Fatal(err)
	}
	owner := uint32(os.Getuid())
	got, err := resolveTrustedOpenWrtLogreadOwnedBy(public, ubox, func(path string) bool {
		return trustedFixtureLogreadPath(path, root, owner)
	}, owner)
	if err != nil {
		t.Fatalf("real inspector fixture was rejected: %v", err)
	}
	if got != ubox {
		t.Fatalf("resolved logread = %q, want %q", got, ubox)
	}
}

func trustedFixtureLogreadPath(path, root string, owner uint32) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || !filepath.IsAbs(root) {
		return false
	}
	for current := filepath.Dir(path); ; {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner {
			return false
		}
		if current == root {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current || parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator)) {
			return false
		}
		current = parent
	}
}

func trustedLogreadFacts(path string, resolved string, symlink bool) logreadCandidateFacts {
	candidate := logreadCandidateFacts{
		path: path, resolvedPath: resolved, exists: true, regular: true,
		symlink: symlink, pathSafe: true, uid: 0, gid: 0, mode: os.FileMode(0o755),
	}
	candidate.trust = &logreadTrustEvidence{
		authority: trustedLogreadAuthority, path: path, resolvedPath: resolved,
		regular: true, uid: 0, gid: 0, mode: os.FileMode(0o755), ancestrySafe: true,
	}
	return candidate
}

func TestOpenWrtLogreadCapabilityUsesOnlyBoundedTrustedCandidates(t *testing.T) {
	validInternal := trustedLogreadFacts(openWrtUboxLogreadPath, openWrtUboxLogreadPath, false)
	validPublic := trustedLogreadFacts(openWrtPublicLogreadPath, openWrtPublicLogreadPath, false)
	validAlternative := trustedLogreadFacts(openWrtPublicLogreadPath, openWrtUboxLogreadPath, true)
	validLegitimateAlternative := trustedLogreadFacts(openWrtPublicLogreadPath, "/usr/libexec/logread-alt", true)
	for name, candidates := range map[string][]logreadCandidateFacts{
		"internal ubox only":               {validInternal},
		"public entrypoint only":           {validPublic},
		"official alternatives symlink":    {validAlternative, validInternal},
		"legitimate alternatives provider": {validLegitimateAlternative},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := chooseTrustedOpenWrtLogread(candidates)
			if err != nil {
				t.Fatalf("trusted candidate set rejected: %v", err)
			}
			if got == "" {
				t.Fatal("trusted candidate resolution returned an empty path")
			}
		})
	}
}

func TestOpenWrtLogreadCapabilityFailsClosedForMissingOrUnsafeCandidates(t *testing.T) {
	validInternal := trustedLogreadFacts(openWrtUboxLogreadPath, openWrtUboxLogreadPath, false)
	tests := map[string][]logreadCandidateFacts{
		"neither candidate":     nil,
		"arbitrary PATH binary": {trustedLogreadFacts("/tmp/logread", "/tmp/logread", false)},
		"synthetic inconsistent trust facts": func() []logreadCandidateFacts {
			candidate := trustedLogreadFacts(openWrtPublicLogreadPath, "/usr/libexec/logread-alt", true)
			candidate.resolvedPath = "/tmp/logread"
			return []logreadCandidateFacts{candidate}
		}(),
		"internal candidate symlink": {trustedLogreadFacts(openWrtUboxLogreadPath, "/tmp/logread", true)},
		"world writable": func() []logreadCandidateFacts {
			candidate := validInternal
			candidate.mode = os.FileMode(0o777)
			return []logreadCandidateFacts{candidate}
		}(),
		"not root owned": func() []logreadCandidateFacts {
			candidate := validInternal
			candidate.uid = 1000
			return []logreadCandidateFacts{candidate}
		}(),
		"not executable regular file": func() []logreadCandidateFacts {
			candidate := validInternal
			candidate.mode = os.FileMode(0o644)
			return []logreadCandidateFacts{candidate}
		}(),
		"non regular entry": func() []logreadCandidateFacts {
			candidate := validInternal
			candidate.regular = false
			return []logreadCandidateFacts{candidate}
		}(),
		"unsafe target ancestry": func() []logreadCandidateFacts {
			candidate := validInternal
			candidate.trust.ancestrySafe = false
			return []logreadCandidateFacts{candidate}
		}(),
	}
	for name, candidates := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := chooseTrustedOpenWrtLogread(candidates); err == nil || got != "" {
				t.Fatalf("unsafe candidate set became authority: %q, %v", got, err)
			}
		})
	}
}

func TestTrustedLogreadSelectionRemainsInsideTheSSHOwner(t *testing.T) {
	composition, err := ResolveRegisteredSSHComposition(ProcdDropbearComposition())
	if err != nil {
		t.Fatal(err)
	}
	if !composition.Valid() || composition.LogEvidence() != LogEvidenceLogread {
		t.Fatal("resolved Dropbear composition lost its owner-local log evidence dimension")
	}
	if _, err := newLogreadSSHLogEvidence(); err != nil && !strings.Contains(err.Error(), "required fixed host binary") {
		t.Fatalf("owner-local logread selection failed outside its bounded capability check: %v", err)
	}
}

func TestOpenWrtLogreadResolverDoesNotUsePathLookup(t *testing.T) {
	source, err := os.ReadFile("log_evidence_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(source) == "" || strings.Contains(string(source), "exec.LookPath") || strings.Contains(string(source), "command -v") {
		t.Fatal("logread resolver must remain bounded to fixed trusted candidates")
	}
}
