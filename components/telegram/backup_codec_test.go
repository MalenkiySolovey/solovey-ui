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
	backupcodec.ResetForTest()
	t.Cleanup(backupcodec.ResetForTest)
	unregister := registerBackupCodecs()

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/backup?backupEncryption="+id, nil)
	name, _, ok := backupcodec.SelectedExport(context)
	if !ok || name != id {
		t.Fatalf("selected export codec = %q, %v", name, ok)
	}
	name, _, ok = backupcodec.MatchingImport([]byte(backupenvelope.Magic))
	if !ok || name != id {
		t.Fatalf("matching import codec = %q, %v", name, ok)
	}

	unregister()
	unregister()
	if _, _, ok := backupcodec.SelectedExport(context); ok {
		t.Fatal("export codec remained registered after cleanup")
	}
	if _, _, ok := backupcodec.MatchingImport([]byte(backupenvelope.Magic)); ok {
		t.Fatal("import codec remained registered after cleanup")
	}
}
