//go:build !minimal

package telegram

import (
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/backupcodec"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/gin-gonic/gin"
)

func TestBackupCodecContributionRegistersAndUnregisters(t *testing.T) {
	unregister, err := registerBackupCodecs()
	if err != nil {
		t.Fatal(err)
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/backup?backupEncryption="+id, nil)
	name, _, ok, err := backupcodec.SelectedExport(context)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || name != id {
		t.Fatalf("selected export codec = %q, %v", name, ok)
	}
	name, _, ok, err = backupcodec.MatchingImport([]byte(backupenvelope.Magic))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || name != id {
		t.Fatalf("matching import codec = %q, %v", name, ok)
	}

	unregister()
	unregister()
	if _, _, ok, err := backupcodec.SelectedExport(context); err != nil || ok {
		t.Fatal("export codec remained registered after cleanup")
	}
	if _, _, ok, err := backupcodec.MatchingImport([]byte(backupenvelope.Magic)); err != nil || ok {
		t.Fatal("import codec remained registered after cleanup")
	}
}
