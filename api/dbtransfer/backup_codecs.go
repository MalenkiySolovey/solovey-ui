package dbtransfer

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/backupcodec"
	"github.com/gin-gonic/gin"
)

type BackupExportContext = backupcodec.ExportContext
type BackupExportResult = backupcodec.ExportResult
type BackupExportCodec = backupcodec.ExportCodec
type BackupExportStreamContext = backupcodec.ExportStreamContext
type BackupImportContext = backupcodec.ImportContext
type BackupImportCodec = backupcodec.ImportCodec
type BackupImportStreamContext = backupcodec.ImportStreamContext
type BackupCodecError = backupcodec.Error

func NewBackupCodecError(status int, class string, err error) error {
	return backupcodec.NewError(status, class, err)
}

func RegisterBackupExportCodec(name string, codec BackupExportCodec) (func(), error) {
	return backupcodec.RegisterExport(name, codec)
}

func RegisterBackupImportCodec(name string, codec BackupImportCodec) (func(), error) {
	return backupcodec.RegisterImport(name, codec)
}

type backupExportCodecEntry struct {
	name  string
	codec BackupExportCodec
}

type backupImportCodecEntry struct {
	name  string
	codec BackupImportCodec
}

func selectedBackupExportCodec(c *gin.Context) (backupExportCodecEntry, bool, error) {
	name, codec, ok, err := backupcodec.SelectedExport(c)
	return backupExportCodecEntry{name: name, codec: codec}, ok, err
}

func backupExportCodecRequested(c *gin.Context) bool {
	return backupcodec.ExportRequested(c)
}

func maxBackupImportCodecHeaderBytes() int {
	return backupcodec.MaxImportHeaderBytes()
}

func backupImportPassphraseFields() []string {
	return backupcodec.ImportPassphraseFields()
}

func matchingBackupImportCodec(header []byte) (backupImportCodecEntry, bool, error) {
	name, codec, ok, err := backupcodec.MatchingImport(header)
	return backupImportCodecEntry{name: name, codec: codec}, ok, err
}

func backupCodecHTTPError(err error, fallbackClass string) (int, string) {
	return backupcodec.HTTPError(err, fallbackClass)
}
