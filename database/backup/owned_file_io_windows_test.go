package backup

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOwnedTreeWindowsShortPathRetainsPrivateFileIdentity(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "long-private-owner-directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	input, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	if err != nil || length >= uint32(len(buffer)) {
		t.Fatal("resolve OS short path", err)
	}
	alias := windows.UTF16ToString(buffer[:length])
	if alias == directory {
		t.Skip("filesystem did not supply a distinct short name")
	}
	before, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(alias)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("short name does not identify the same owner directory")
	}
	name, err := PublishOwnedTree(filepath.Join(alias, "restored"), map[string][]byte{"fixture": []byte("fixture")}, true)
	if err != nil {
		t.Fatal("publish through the same private OS directory", err)
	}
	data, err := ReadOwnedFile(name, filepath.Join(name, "fixture"))
	if err != nil || string(data) != "fixture" {
		t.Fatal("read through the same private OS directory", err)
	}
}

func TestOwnedTreeWindowsActualSymlinkRemainsRejected(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Skip("host cannot create a test-only symlink")
	}
	if _, err := PublishOwnedTree(filepath.Join(link, "restored"), map[string][]byte{"fixture": []byte("fixture")}, true); err == nil {
		t.Fatal("actual symlink accepted for publication")
	}
	name := filepath.Join(actual, "fixture")
	if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(root, filepath.Join(link, "fixture")); err == nil {
		t.Fatal("actual symlink accepted for reading")
	}
}
