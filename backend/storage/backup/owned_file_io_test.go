package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImmutableOwnerPublicationPreservesRollback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner", "restored")
	name, err := PublishOwnedFile(root, []byte("new"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("rehearsal published files")
	}
	actual, err := PublishOwnedFile(root, []byte("new"), true)
	if err != nil || name != actual {
		t.Fatal(err)
	}
	if _, err := PublishOwnedFile(root, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("conflict"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishOwnedFile(root, []byte("new"), true); err == nil {
		t.Fatal("conflicting immutable file overwritten")
	}
	data, _ := os.ReadFile(name)
	if string(data) != "conflict" {
		t.Fatal("pre-existing data changed")
	}
}
