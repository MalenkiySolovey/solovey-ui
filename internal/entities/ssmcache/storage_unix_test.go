//go:build !windows

package ssmcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRejectsPublicFilesDirectoriesLinksAndPreservesThem(t *testing.T) {
	s := testStore(t, "state.json")
	if err := s.Write(context.Background(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(Root(), "state.json")
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(name); err == nil {
		t.Fatal("public existing file accepted")
	}
	if _, err := s.Read(context.Background()); err == nil {
		t.Fatal("public read accepted")
	}
	if err := s.Write(context.Background(), []byte("{}")); err == nil {
		t.Fatal("public preimage overwritten")
	}
	info, _ := os.Stat(name)
	if info.Mode().Perm() != 0644 {
		t.Fatal("existing permissions silently changed")
	}
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(name, filepath.Join(Root(), "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); err == nil {
		t.Fatal("hard-linked credential state accepted")
	}
	if err := os.Remove(filepath.Join(Root(), "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(name, filepath.Join(Root(), "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(Root(), "link")); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Mkdir(filepath.Join(Root(), "parent"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(Root(), "parent"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(Root(), "parent", "state")); err == nil {
		t.Fatal("public ancestor accepted")
	}
	if err := os.Symlink(filepath.Join(Root(), "parent"), filepath.Join(Root(), "parent-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(Root(), "parent-link", "state")); err == nil {
		t.Fatal("indirect symlink accepted")
	}
	if err := os.Chmod(Root(), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := New(name); err == nil {
		t.Fatal("public root accepted")
	}
}
