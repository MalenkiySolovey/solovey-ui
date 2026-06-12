package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

const maxDatabaseImportBytes = 64 << 20

type memoryMultipartFile struct {
	*bytes.Reader
}

func (f memoryMultipartFile) Close() error {
	return nil
}

type preparedDatabaseImportFile struct {
	file    multipart.File
	cleanup func()
}

func (f preparedDatabaseImportFile) Close() {
	if f.file != nil {
		_ = f.file.Close()
	}
	if f.cleanup != nil {
		f.cleanup()
	}
}

func (a *ApiService) openDatabaseImportFile(c *gin.Context) (preparedDatabaseImportFile, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDatabaseImportBytes)
	file, _, err := c.Request.FormFile("db")
	if err != nil {
		a.respondDatabaseImportFailure(c, err)
		jsonMsg(c, "", err)
		return preparedDatabaseImportFile{}, false
	}
	prepared, ok := a.prepareDatabaseImportFile(c, file)
	if !ok {
		_ = file.Close()
		return preparedDatabaseImportFile{}, false
	}
	return prepared, true
}

func (a *ApiService) prepareDatabaseImportFile(c *gin.Context, file multipart.File) (preparedDatabaseImportFile, bool) {
	header := make([]byte, len(service.TelegramBackupMagic))
	n, readErr := io.ReadFull(file, header)
	if seekErr := seekMultipartFileStart(file); seekErr != nil {
		a.respondDatabaseImportFailure(c, seekErr)
		jsonMsg(c, "", seekErr)
		return preparedDatabaseImportFile{}, false
	}
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		a.respondDatabaseImportFailure(c, readErr)
		jsonMsg(c, "", readErr)
		return preparedDatabaseImportFile{}, false
	}
	if !service.IsTelegramBackupEnvelope(header[:n]) {
		return preparedDatabaseImportFile{file: file}, true
	}
	return a.prepareTelegramBackupRestoreFile(c, file)
}

func seekMultipartFileStart(file multipart.File) error {
	if _, err := file.Seek(0, 0); err != nil {
		return common.NewErrorf("Error resetting file reader: %v", err)
	}
	return nil
}
