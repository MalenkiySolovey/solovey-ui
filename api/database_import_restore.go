package api

import (
	"bytes"
	"io"
	"mime/multipart"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) prepareTelegramBackupRestoreFile(c *gin.Context, source multipart.File) (preparedDatabaseImportFile, bool) {
	passphrase := telegramBackupRestorePassphrase(c)
	defer wipeBytes(passphrase)
	if len(passphrase) == 0 {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
		return preparedDatabaseImportFile{}, false
	}

	envelope, err := io.ReadAll(source)
	if err != nil {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
		return preparedDatabaseImportFile{}, false
	}
	defer wipeBytes(envelope)

	plaintext, err := service.OpenTelegramBackupEnvelope(envelope, passphrase)
	if err != nil {
		a.respondTelegramBackupRestoreDecryptionFailed(c)
		return preparedDatabaseImportFile{}, false
	}
	return preparedDatabaseImportFile{
		file: memoryMultipartFile{Reader: bytes.NewReader(plaintext)},
		cleanup: func() {
			_ = source.Close()
			wipeBytes(plaintext)
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
