package openwrt

import (
	"path/filepath"
	"strings"
	"testing"

	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

func TestOpenWrtDataLifecycleRecoveryUsesPersistentBoundedOwnerRoots(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", "/etc/solovey-ui")
	manager := datalifecycle.NewManager()
	wantDrop := filepath.Clean("/etc/solovey-ui/recovery/drop-data")
	wantRestore := filepath.Clean("/etc/solovey-ui/recovery/restore")
	if filepath.Clean(manager.Root) != wantDrop || filepath.Clean(manager.RestoreRoot) != wantRestore ||
		strings.HasPrefix(filepath.Clean(manager.Root), filepath.Clean("/tmp")) || strings.HasPrefix(filepath.Clean(manager.Root), filepath.Clean("/run")) {
		t.Fatalf("OpenWrt recovery roots drop=%q restore=%q", manager.Root, manager.RestoreRoot)
	}
	policy := datalifecycle.HistoryRetentionPolicy()
	if policy.TerminalOperations <= 0 || policy.TerminalAgeSeconds <= 0 || policy.TerminalLogicalBytes <= 0 ||
		policy.TerminalArtifacts <= 0 || policy.TerminalArtifactBytes <= 0 || policy.TerminalArtifactBytes > 512<<20 {
		t.Fatalf("unbounded persistent data-lifecycle policy=%#v", policy)
	}
}
