//go:build linux

package nftables

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNFTOwnerBindSafeObjectsAndRejectAmbiguity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux executable fixture is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-helper-executable-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := copyHelperExecutable(t, root, "owner-one")
	alias := filepath.Join(root, "owner-alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	nft := openNFTExecutable([]string{first, alias})
	if nft == nil || nft.Identity().Digest == "" {
		t.Fatal("nft owner rejected one object published through a safe alias")
	}
	_ = nft.Close()
	second := copyHelperExecutable(t, root, "owner-two")
	if nft := openNFTExecutable([]string{first, second}); nft != nil {
		_ = nft.Close()
		t.Fatal("distinct nft candidates were accepted as one authority")
	}

}

func copyHelperExecutable(t *testing.T, root, name string) string {
	t.Helper()
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	path := filepath.Join(root, name)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
