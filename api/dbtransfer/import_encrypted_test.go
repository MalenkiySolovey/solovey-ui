package dbtransfer

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"

	"github.com/gin-gonic/gin"
)

func TestPrepareDatabaseImportFileDecryptsBackupPassphraseAlias(t *testing.T) {
	registerTelegramImportCodecForTest(t)

	plaintext := []byte("not-a-real-db-but-decrypted")
	passphrase := []byte("restore alias passphrase")
	envelope, err := backupenvelope.Build(plaintext, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	form := url.Values{"backupPassphrase": {string(passphrase)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/importdb", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	prepared, ok := (&Handler{JSONMsg: func(*gin.Context, string, error) {}}).prepareDatabaseImportFile(c, memoryMultipartFile{Reader: bytes.NewReader(envelope)})
	if !ok {
		t.Fatal("encrypted import file was not prepared")
	}
	defer prepared.Close()

	got, err := io.ReadAll(prepared.file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("decrypted restore payload=%q, want %q", string(got), string(plaintext))
	}
}

func TestPrepareDatabaseImportFilePrefersDedicatedPassphraseField(t *testing.T) {
	registerTelegramImportCodecForTest(t)

	plaintext := []byte("dedicated field wins")
	envelope, err := backupenvelope.Build(plaintext, []byte("primary"))
	if err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	form := url.Values{
		"telegramBackupPassphrase": {"primary"},
		"backupPassphrase":         {"legacy"},
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/importdb", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	prepared, ok := (&Handler{JSONMsg: func(*gin.Context, string, error) {}}).prepareDatabaseImportFile(c, memoryMultipartFile{Reader: bytes.NewReader(envelope)})
	if !ok {
		t.Fatal("encrypted import file was not prepared")
	}
	defer prepared.Close()

	got, err := io.ReadAll(prepared.file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("decrypted restore payload=%q, want %q", string(got), string(plaintext))
	}
}

func registerTelegramImportCodecForTest(t *testing.T) {
	t.Helper()
	ResetBackupCodecsForTest()
	t.Cleanup(ResetBackupCodecsForTest)
	unregister := RegisterBackupImportCodec("test-telegram", BackupImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		Match:             backupenvelope.IsEnvelope,
		FailureAuditEvent: "tg_backup_restore_failed",
		Decode: func(ctx BackupImportContext) ([]byte, error) {
			passphrase := ctx.Gin.PostForm("telegramBackupPassphrase")
			if passphrase == "" {
				passphrase = ctx.Gin.PostForm("backupPassphrase")
			}
			if passphrase == "" {
				return nil, NewBackupCodecError(http.StatusBadRequest, "decryption_failed", nil)
			}
			return backupenvelope.Open(ctx.Payload, []byte(passphrase))
		},
	})
	t.Cleanup(unregister)
}
