package importxui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/gin-gonic/gin"
)

const (
	maxXUIImportBytes = 200 << 20
	MaxFieldBytes     = 8 << 20

	xuiUploadTempPrefix          = "xui-import-"
	xuiUploadTempMaxAge          = 24 * time.Hour
	xuiUploadTempCleanupInterval = time.Hour
)

type xuiFieldTooLargeError struct {
	Field string
	Limit int64
}

func (e *xuiFieldTooLargeError) Error() string {
	return "payload_too_large: field " + e.Field + " exceeds " + strconv.FormatInt(e.Limit, 10) + " bytes"
}

var (
	xuiUploadTempRoot = os.TempDir
	xuiUploadNow      = time.Now
	xuiUploadCleanup  = cleanupStaleXUIUploads

	xuiUploadCleanupMu   sync.Mutex
	xuiUploadLastCleanup time.Time
)

func saveUpload(c *gin.Context) (*Upload, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxXUIImportBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, err
	}
	maybeCleanupStaleXUIUploads()
	dir, err := os.MkdirTemp(xuiUploadTempRoot(), xuiUploadTempPrefix+"*")
	if err != nil {
		return nil, err
	}
	upload := &Upload{Dir: dir, Fields: map[string]string{}}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		name := part.FormName()
		if name == "db" {
			path := filepath.Join(dir, "source.db")
			// #nosec G304 -- path is a fixed name under the per-request upload temp directory.
			out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			hash := sha256.New()
			_, copyErr := io.Copy(out, io.TeeReader(part, hash))
			closeErr := out.Close()
			if copyErr != nil {
				_ = os.RemoveAll(dir)
				return nil, copyErr
			}
			if closeErr != nil {
				_ = os.RemoveAll(dir)
				return nil, closeErr
			}
			if err := validateSQLiteFile(path); err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			upload.Path = path
			upload.SHA256 = hex.EncodeToString(hash.Sum(nil))
			continue
		}
		if name == "plan" {
			path := filepath.Join(dir, "plan.json")
			// #nosec G304 -- path is constrained to the per-request upload temp directory.
			out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			size, copyErr := io.Copy(out, part)
			closeErr := out.Close()
			if copyErr != nil {
				_ = os.RemoveAll(dir)
				return nil, copyErr
			}
			if closeErr != nil {
				_ = os.RemoveAll(dir)
				return nil, closeErr
			}
			upload.PlanPath = path
			upload.PlanSize = size
			continue
		}
		value, err := readXUIField(part, name, MaxFieldBytes)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		upload.Fields[name] = value
	}
	if upload.Path == "" {
		_ = os.RemoveAll(dir)
		return nil, errors.New("missing db file")
	}
	return upload, nil
}

func maybeCleanupStaleXUIUploads() {
	root := xuiUploadTempRoot()
	now := xuiUploadNow()

	xuiUploadCleanupMu.Lock()
	sinceLast := now.Sub(xuiUploadLastCleanup)
	if !xuiUploadLastCleanup.IsZero() && sinceLast >= 0 && sinceLast < xuiUploadTempCleanupInterval {
		xuiUploadCleanupMu.Unlock()
		return
	}
	xuiUploadLastCleanup = now
	xuiUploadCleanupMu.Unlock()

	if err := xuiUploadCleanup(root, now, xuiUploadTempMaxAge); err != nil {
		logger.Warning("xui import stale upload cleanup failed:", err)
	}
}

func cleanupStaleXUIUploads(root string, now time.Time, maxAge time.Duration) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var cleanupErrs []error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, xuiUploadTempPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			cleanupErrs = append(cleanupErrs, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		path := filepath.Join(root, name)
		info, err = os.Lstat(path)
		if err != nil {
			cleanupErrs = append(cleanupErrs, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		// #nosec G304 -- path is constrained to entries read from the temp root with the xui-import prefix.
		if err := os.RemoveAll(path); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	return errors.Join(cleanupErrs...)
}

func readXUIField(part *multipart.Part, name string, limit int64) (string, error) {
	value, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(value)) > limit {
		return "", &xuiFieldTooLargeError{Field: name, Limit: limit}
	}
	return string(value), nil
}

func validateSQLiteFile(path string) error {
	// #nosec G304 -- path is constrained to the per-request upload temp directory.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	ok, err := backup.IsSQLite(file)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("not_sqlite")
	}
	return nil
}
