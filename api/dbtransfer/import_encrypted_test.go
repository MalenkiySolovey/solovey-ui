package dbtransfer

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"

	"github.com/gin-gonic/gin"
)

func TestPrepareDatabaseImportFileDecryptsBackupPassphraseAlias(t *testing.T) {
	plaintext := []byte("not-a-real-db-but-decrypted")
	passphrase := []byte("restore alias passphrase")
	envelope, err := integrationtelegram.BuildTelegramBackupEnvelope(plaintext, passphrase)
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

func TestTelegramBackupRestorePassphrasePrefersDedicatedField(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	form := url.Values{
		"telegramBackupPassphrase": {"primary"},
		"backupPassphrase":         {"legacy"},
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/importdb", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got := string(telegramBackupRestorePassphrase(c))
	if got != "primary" {
		t.Fatalf("restore passphrase=%q, want primary", got)
	}
}
