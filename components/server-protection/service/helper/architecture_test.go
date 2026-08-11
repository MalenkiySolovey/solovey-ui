package helper

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessExecutionExistsOnlyInRestrictedHelperAdapter(t *testing.T) {
	componentRoot := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(componentRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte(`"os/exec"`)) && !bytes.Contains(data, []byte("exec.Command")) {
			return nil
		}
		cleanPath := filepath.ToSlash(path)
		if !strings.HasSuffix(cleanPath, "/service/helper/process.go") && !strings.HasSuffix(cleanPath, "/service/helper/nft_backend.go") && !strings.HasSuffix(cleanPath, "/service/helper/nginx_backend.go") && !strings.HasSuffix(cleanPath, "/service/helper/ssh_recovery_backend.go") && !strings.HasSuffix(cleanPath, "/service/helper/listener_owner_linux.go") {
			t.Errorf("process execution escaped restricted helper adapter: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
