package importxui

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestValidateRollbackPathRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	outside := filepath.Join(t.TempDir(), "outside.db")
	if err := os.WriteFile(outside, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "s-ui-pre-xui-import-1.db")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := validateRollbackPath(symlink); err == nil {
		t.Fatal("expected symlink rollback path to be rejected")
	}
}

func TestValidateRollbackPathAllowsRealBackupInDatabaseDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	backup := filepath.Join(dir, "s-ui-pre-xui-import-1.db")
	if err := os.WriteFile(backup, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRollbackPath(backup); err != nil {
		t.Fatalf("expected rollback path to be accepted: %v", err)
	}
}

func TestCleanupStaleXUIUploadsRemovesOnlyOldImportDirsIssue38(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	oldTime := now.Add(-xuiUploadTempMaxAge - time.Minute)
	boundaryTime := now.Add(-xuiUploadTempMaxAge)
	freshTime := now.Add(-time.Minute)

	oldImportDir := filepath.Join(root, xuiUploadTempPrefix+"old")
	freshImportDir := filepath.Join(root, xuiUploadTempPrefix+"fresh")
	boundaryImportDir := filepath.Join(root, xuiUploadTempPrefix+"boundary")
	unrelatedOldDir := filepath.Join(root, "other-import-old")
	nestedImportDir := filepath.Join(root, "nested", xuiUploadTempPrefix+"old")
	importFile := filepath.Join(root, xuiUploadTempPrefix+"file")
	symlinkTarget := filepath.Join(root, "old-symlink-target")
	symlinkImportDir := filepath.Join(root, xuiUploadTempPrefix+"symlink")
	for _, dir := range []string{oldImportDir, freshImportDir, boundaryImportDir, unrelatedOldDir, nestedImportDir, symlinkTarget} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldImportDir, "payload.db"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(importFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkCreated := false
	if err := os.Symlink(symlinkTarget, symlinkImportDir); err == nil {
		symlinkCreated = true
	} else {
		t.Logf("symlink unavailable, skipping symlink assertion: %v", err)
	}
	for _, path := range []string{oldImportDir, unrelatedOldDir, nestedImportDir, symlinkTarget, importFile} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(boundaryImportDir, boundaryTime, boundaryTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freshImportDir, freshTime, freshTime); err != nil {
		t.Fatal(err)
	}

	if err := cleanupStaleXUIUploads(root, now, xuiUploadTempMaxAge); err != nil {
		t.Fatal(err)
	}

	assertPathMissing(t, oldImportDir)
	for _, path := range []string{freshImportDir, boundaryImportDir, unrelatedOldDir, nestedImportDir, importFile, symlinkTarget} {
		assertPathExists(t, path)
	}
	if symlinkCreated {
		assertPathExists(t, symlinkImportDir)
	}
}

func TestSaveXUIUploadTriggersStaleCleanupIssue38(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	resetXUIUploadCleanupForTest()
	prevRoot := xuiUploadTempRoot
	prevNow := xuiUploadNow
	xuiUploadTempRoot = func() string { return root }
	xuiUploadNow = func() time.Time { return now }
	t.Cleanup(func() {
		xuiUploadTempRoot = prevRoot
		xuiUploadNow = prevNow
		resetXUIUploadCleanupForTest()
	})

	staleDir := filepath.Join(root, xuiUploadTempPrefix+"stale")
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "payload.db"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-xuiUploadTempMaxAge - time.Minute)
	if err := os.Chtimes(staleDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = newXuiImportRequest(t, "/api/import-xui", []byte("SQLite format 3\x00"), "1")

	upload, err := saveUpload(c)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(upload.Dir)

	assertPathMissing(t, staleDir)
	assertPathExists(t, upload.Dir)
	if filepath.Dir(upload.Path) != upload.Dir {
		t.Fatalf("upload path %q is not under upload dir %q", upload.Path, upload.Dir)
	}
	if !strings.HasPrefix(upload.Dir, root+string(os.PathSeparator)) {
		t.Fatalf("upload dir %q is not under temp root %q", upload.Dir, root)
	}
	if upload.SHA256 == "" {
		t.Fatal("upload SHA256 was not populated")
	}
}

func TestSaveXUIUploadCleanupIsFailSoftIssue38(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	resetXUIUploadCleanupForTest()
	prevRoot := xuiUploadTempRoot
	prevNow := xuiUploadNow
	prevCleanup := xuiUploadCleanup
	cleanupErr := errors.New("cleanup failed")
	cleanupCalls := 0
	xuiUploadTempRoot = func() string { return root }
	xuiUploadNow = func() time.Time { return now }
	xuiUploadCleanup = func(root string, now time.Time, maxAge time.Duration) error {
		cleanupCalls++
		return cleanupErr
	}
	t.Cleanup(func() {
		xuiUploadTempRoot = prevRoot
		xuiUploadNow = prevNow
		xuiUploadCleanup = prevCleanup
		resetXUIUploadCleanupForTest()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = newXuiImportRequest(t, "/api/import-xui", []byte("SQLite format 3\x00"), "1")

	upload, err := saveUpload(c)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(upload.Dir)
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls=%d, want 1", cleanupCalls)
	}
	assertPathExists(t, upload.Dir)
	if upload.SHA256 == "" {
		t.Fatal("upload SHA256 was not populated")
	}
}

func newXuiImportRequest(t *testing.T, path string, content []byte, dryRun string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if dryRun != "" {
		if err := writer.WriteField("dryRun", dryRun); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.WriteField("strategy", "merge"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("db", "x-ui.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func resetXUIUploadCleanupForTest() {
	xuiUploadCleanupMu.Lock()
	defer xuiUploadCleanupMu.Unlock()
	xuiUploadLastCleanup = time.Time{}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %q to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %q to be removed, stat err=%v", path, err)
	}
}
