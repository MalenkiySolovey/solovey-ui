package dbtransfer

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"

	"github.com/gin-gonic/gin"
)

func TestDatabaseImportFailsClosedWithoutBrowserStepUpCapability(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/apiv2/importdb", strings.NewReader(""))
	handler := NewHandler(Deps{
		RequireScope: func(*gin.Context, string, ...string) bool { return true },
	})

	handler.ImportDb(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("import without browser step-up status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestValidateDatabaseImportMultipartBoundsPartsFieldsAndFiles(t *testing.T) {
	valid := &multipart.Form{
		Value: map[string][]string{"backupPassphrase": {"bounded secret"}},
		File:  map[string][]*multipart.FileHeader{"db": {{Filename: "backup.db"}}},
	}
	if err := validateDatabaseImportMultipart(valid); err != nil {
		t.Fatalf("valid multipart rejected: %v", err)
	}
	tests := []struct {
		name string
		form *multipart.Form
	}{
		{
			name: "duplicate file",
			form: &multipart.Form{File: map[string][]*multipart.FileHeader{
				"db": {{Filename: "one.db"}, {Filename: "two.db"}},
			}},
		},
		{
			name: "unknown field",
			form: &multipart.Form{
				Value: map[string][]string{"unexpected": {"value"}},
				File:  map[string][]*multipart.FileHeader{"db": {{Filename: "backup.db"}}},
			},
		},
		{
			name: "oversized field",
			form: &multipart.Form{
				Value: map[string][]string{"backupPassphrase": {strings.Repeat("x", maxPassphraseBytes+1)}},
				File:  map[string][]*multipart.FileHeader{"db": {{Filename: "backup.db"}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDatabaseImportMultipart(test.form); err == nil {
				t.Fatal("invalid multipart was accepted")
			}
		})
	}
}

func TestPrepareDatabaseImportFileDecryptsBackupPassphraseAlias(t *testing.T) {
	registerFixtureImportCodecForTest(t)

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
	registerFixtureImportCodecForTest(t)

	plaintext := []byte("dedicated field wins")
	envelope, err := backupenvelope.Build(plaintext, []byte("primary"))
	if err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	form := url.Values{
		"fixtureBackupPassphrase": {"primary"},
		"backupPassphrase":        {"legacy"},
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

func registerFixtureImportCodecForTest(t *testing.T) {
	t.Helper()
	unregister, err := RegisterBackupImportCodec("test-fixture-codec", BackupImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		PassphraseFields:  []string{"fixtureBackupPassphrase"},
		Match:             backupenvelope.IsEnvelope,
		FailureAuditEvent: "fixture_backup_restore_failed",
		Decode: func(ctx BackupImportContext) ([]byte, error) {
			passphrase := ctx.Gin.PostForm("fixtureBackupPassphrase")
			if passphrase == "" {
				passphrase = ctx.Gin.PostForm("backupPassphrase")
			}
			if passphrase == "" {
				return nil, NewBackupCodecError(http.StatusBadRequest, "decryption_failed", nil)
			}
			return backupenvelope.Open(ctx.Payload, []byte(passphrase))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
}
