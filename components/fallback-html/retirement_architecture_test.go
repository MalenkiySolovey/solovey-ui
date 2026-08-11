//go:build !minimal

package fallbackhtml

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacySelfStealWritePathAndReverseImportAreAbsent(t *testing.T) {
	forbidden := []string{
		"CreateSelfStealDraft",
		"saveSelfStealTransferTarget",
		"createSelfStealTLSRecord",
		"planSelfStealPortTransfer",
		"generateSelfStealRealityKeyPair",
		`native-fallback/preview`,
		`native-fallback/prepare`,
		`native-fallback/apply`,
	}
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".test.ts") ||
			(!strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".vue")) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range forbidden {
			if strings.Contains(string(content), marker) {
				t.Fatalf("retired or forbidden production marker %q remains in %s", marker, path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
