package dbtransfer

import (
	"bytes"
	"io"
	"mime/multipart"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) prepareTelegramBackupRestoreFile(c *gin.Context, source multipart.File) (preparedDatabaseImportFile, bool) {
	passphrase := telegramBackupRestorePassphrase(c)
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
		return preparedDatabaseImportFile{}, false
	}

	envelope, err := io.ReadAll(source)
	if err != nil {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
		return preparedDatabaseImportFile{}, false
	}
	defer common.WipeBytes(envelope)

	plaintext, err := integrationtelegram.OpenTelegramBackupEnvelope(envelope, passphrase)
	if err != nil {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
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

func telegramBackupRestorePassphrase(c *gin.Context) []byte {
	passphraseValue := c.PostForm("telegramBackupPassphrase")
	if passphraseValue == "" {
		passphraseValue = c.PostForm("backupPassphrase")
	}
	return []byte(passphraseValue)
}
