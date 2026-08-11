package dbtransfer

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

const (
	maxDatabaseImportBytes = 512 << 20
	multipartMemoryBytes   = 1 << 20
	maxDatabaseFormParts   = 8
	maxPassphraseBytes     = 4 << 10
	maxRestoreControlBytes = 128
)

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

func (f preparedDatabaseImportFile) MultipartFile() multipart.File {
	return f.file
}

func (a *Handler) openDatabaseImportFile(c *gin.Context) (preparedDatabaseImportFile, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDatabaseImportBytes)
	if err := c.Request.ParseMultipartForm(multipartMemoryBytes); err != nil {
		a.respondDatabaseImportFailure(c, err)
		a.JSONMsg(c, "", err)
		return preparedDatabaseImportFile{}, false
	}
	if err := validateDatabaseImportMultipart(c.Request.MultipartForm); err != nil {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
		a.respondDatabaseImportFailure(c, err)
		a.JSONMsg(c, "", err)
		return preparedDatabaseImportFile{}, false
	}
	cleanupMultipart := func() {}
	if c.Request.MultipartForm != nil {
		cleanupMultipart = func() { _ = c.Request.MultipartForm.RemoveAll() }
	}
	file, _, err := c.Request.FormFile("db")
	if err != nil {
		cleanupMultipart()
		a.respondDatabaseImportFailure(c, err)
		a.JSONMsg(c, "", err)
		return preparedDatabaseImportFile{}, false
	}
	prepared, ok := a.prepareDatabaseImportFile(c, file)
	if !ok {
		_ = file.Close()
		cleanupMultipart()
		return preparedDatabaseImportFile{}, false
	}
	previousCleanup := prepared.cleanup
	prepared.cleanup = func() {
		if previousCleanup != nil {
			previousCleanup()
		}
		cleanupMultipart()
	}
	return prepared, true
}

func validateDatabaseImportMultipart(form *multipart.Form) error {
	if form == nil {
		return common.NewError("invalid database import form")
	}
	passphraseFields := map[string]struct{}{"backupPassphrase": {}}
	for _, field := range backupImportPassphraseFields() {
		passphraseFields[field] = struct{}{}
	}
	controlFields := map[string]struct{}{
		"expectedRehearsalRevision": {},
		"idempotencyKey":            {},
		"confirmation":              {},
		"acknowledged":              {},
	}
	partCount := 0
	for key, values := range form.Value {
		_, passphraseField := passphraseFields[key]
		_, controlField := controlFields[key]
		if !passphraseField && !controlField {
			return common.NewError("unsupported database import field")
		}
		partCount += len(values)
		if len(values) > 1 {
			return common.NewError("duplicate database import field")
		}
		for _, value := range values {
			limit := maxRestoreControlBytes
			if passphraseField {
				limit = maxPassphraseBytes
			}
			if len(value) > limit {
				return common.NewError("database import field is too large")
			}
		}
	}
	for key, files := range form.File {
		if key != "db" {
			return common.NewError("unsupported database import file")
		}
		partCount += len(files)
		if len(files) != 1 {
			return common.NewError("database import requires one file")
		}
	}
	if partCount < 1 || partCount > maxDatabaseFormParts || len(form.File["db"]) != 1 {
		return common.NewError("invalid database import part count")
	}
	return nil
}

func (a *Handler) prepareDatabaseImportFile(c *gin.Context, file multipart.File) (preparedDatabaseImportFile, bool) {
	headerSize := maxBackupImportCodecHeaderBytes()
	if headerSize <= 0 {
		return preparedDatabaseImportFile{file: file}, true
	}
	header := make([]byte, headerSize)
	n, readErr := io.ReadFull(file, header)
	if seekErr := seekMultipartFileStart(file); seekErr != nil {
		a.respondDatabaseImportFailure(c, seekErr)
		a.JSONMsg(c, "", seekErr)
		return preparedDatabaseImportFile{}, false
	}
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		a.respondDatabaseImportFailure(c, readErr)
		a.JSONMsg(c, "", readErr)
		return preparedDatabaseImportFile{}, false
	}
	codec, ok := matchingBackupImportCodec(header[:n])
	if !ok {
		return preparedDatabaseImportFile{file: file}, true
	}
	return a.prepareBackupCodecRestoreFile(c, file, codec)
}

func seekMultipartFileStart(file multipart.File) error {
	if _, err := file.Seek(0, 0); err != nil {
		return common.NewErrorf("Error resetting file reader: %v", err)
	}
	return nil
}
