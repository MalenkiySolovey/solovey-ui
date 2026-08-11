package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyApplyPipelineIsNonMutatingAndRequiresBroker(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	if err := os.WriteFile(executable, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := ApplyPipeline(ReleaseTarget{Version: "9999.0.0", AssetURL: "https://example.invalid/artifact"},
		PipelineDeps{ExecPath: executable}, func(UpdateStage) {})
	if !errors.Is(err, ErrBrokerRequired) {
		t.Fatalf("error = %v", err)
	}
	content, readErr := os.ReadFile(executable)
	if readErr != nil || string(content) != "OLD" {
		t.Fatalf("legacy pipeline changed executable: %q, %v", content, readErr)
	}
}

func TestLegacyRollbackAndPendingMarkersAreDisabled(t *testing.T) {
	if !errors.Is(RestoreBackup("ignored"), ErrBrokerRequired) {
		t.Fatal("legacy rollback did not require broker")
	}
	if CheckPending("ignored") {
		t.Fatal("legacy pending marker unexpectedly triggered")
	}
	ClearPending("ignored")
}
