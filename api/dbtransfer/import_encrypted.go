package dbtransfer

import (
	"bytes"
	"io"
	"mime/multipart"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) prepareBackupCodecRestoreFile(c *gin.Context, source multipart.File, codec backupImportCodecEntry) (preparedDatabaseImportFile, bool) {
	payload, err := io.ReadAll(source)
	if err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	defer common.WipeBytes(payload)

	plaintext, err := codec.codec.Decode(BackupImportContext{
		Gin:     c,
		Payload: payload,
	})
	if err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	return preparedDatabaseImportFile{
		file: memoryMultipartFile{Reader: bytes.NewReader(plaintext)},
		cleanup: func() {
			_ = source.Close()
			common.WipeBytes(plaintext)
		},
	}, true
}
