//go:build !minimal

package telegramcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
)

func TestDecryptBackupCommandRoundTripWithEnvPassphrase(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("sqlite payload bytes")
	passphrase := "correct horse battery staple"
	inPath := filepath.Join(dir, "backup.db.aes")
	outPath := filepath.Join(dir, "backup.db")
	envelope, err := backupenvelope.Build(payload, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunDecryptBackup([]string{"--in", inPath, "--out", outPath, "--passphrase-env", "SUI_TEST_PASSPHRASE"}, strings.NewReader(""), &stdout, &stderr, func(name string) string {
		if name == "SUI_TEST_PASSPHRASE" {
			return passphrase
		}
		return ""
	})
	if code != 0 {
		t.Fatalf("unexpected exit code %d stderr=%s", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decrypted payload mismatch: %q", got)
	}
	if strings.Contains(stdout.String(), passphrase) || strings.Contains(stderr.String(), passphrase) {
		t.Fatalf("passphrase leaked to command output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestDecryptBackupCommandRoundTripWithStdinPassphrase(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("payload from stdin passphrase")
	passphrase := "correct horse battery staple"
	inPath := filepath.Join(dir, "backup.db.aes")
	outPath := filepath.Join(dir, "backup.db")
	envelope, err := backupenvelope.Build(payload, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunDecryptBackup([]string{"--in", inPath, "--out", outPath, "--passphrase-stdin"}, strings.NewReader(passphrase+"\n"), &stdout, &stderr, os.Getenv)
	if code != 0 {
		t.Fatalf("unexpected exit code %d stderr=%s", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decrypted payload mismatch: %q", got)
	}
}

func TestDecryptBackupCommandWrongPassphraseRemovesPartialOutput(t *testing.T) {
	dir := t.TempDir()
	passphrase := "correct horse battery staple"
	inPath := filepath.Join(dir, "backup.db.aes")
	outPath := filepath.Join(dir, "backup.db")
	envelope, err := backupenvelope.Build([]byte("payload"), []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunDecryptBackup([]string{"--in", inPath, "--out", outPath, "--passphrase-env", "SUI_TEST_PASSPHRASE"}, strings.NewReader(""), &stdout, &stderr, func(string) string {
		return "wrong passphrase"
	})
	if code == 0 {
		t.Fatalf("expected failure")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("partial output was not removed, stat err=%v stderr=%s", err, stderr.String())
	}
}

func TestDecryptBackupCommandRequiresInputAndOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunDecryptBackup([]string{"--in", "x"}, strings.NewReader(""), &stdout, &stderr, os.Getenv)
	if code != 2 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--in and --out are required") {
		t.Fatalf("unexpected stderr=%q", stderr.String())
	}
}
