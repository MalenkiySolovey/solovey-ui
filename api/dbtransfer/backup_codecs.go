package dbtransfer

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type BackupExportContext struct {
	Gin     *gin.Context
	Exclude string
	Plain   []byte
}

type BackupExportResult struct {
	Payload       []byte
	Encrypted     bool
	AuditEvent    string
	AuditSeverity string
	AuditDetails  map[string]any
}

type BackupExportCodec struct {
	Selected  func(*gin.Context) bool
	Preflight func(*gin.Context) error
	Encode    func(BackupExportContext) (BackupExportResult, error)
}

type BackupImportContext struct {
	Gin     *gin.Context
	Payload []byte
}

type BackupImportCodec struct {
	HeaderBytes       int
	Match             func([]byte) bool
	Decode            func(BackupImportContext) ([]byte, error)
	FailureAuditEvent string
}

type BackupCodecError struct {
	Status int
	Class  string
	Err    error
}

func NewBackupCodecError(status int, class string, err error) error {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if class == "" {
		class = "failed"
	}
	return &BackupCodecError{Status: status, Class: class, Err: err}
}

func (e *BackupCodecError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Class, e.Err)
	}
	return e.Class
}

func (e *BackupCodecError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type backupExportCodecEntry struct {
	name  string
	codec BackupExportCodec
}

type backupImportCodecEntry struct {
	name  string
	codec BackupImportCodec
}

var backupCodecs = struct {
	sync.RWMutex
	export  map[string]BackupExportCodec
	imports map[string]BackupImportCodec
}{
	export:  map[string]BackupExportCodec{},
	imports: map[string]BackupImportCodec{},
}

func RegisterBackupExportCodec(name string, codec BackupExportCodec) func() {
	if name == "" {
		panic("backup export codec name is required")
	}
	if codec.Selected == nil {
		panic(fmt.Errorf("backup export codec %q selector is required", name))
	}
	if codec.Encode == nil {
		panic(fmt.Errorf("backup export codec %q encoder is required", name))
	}

	backupCodecs.Lock()
	if _, exists := backupCodecs.export[name]; exists {
		backupCodecs.Unlock()
		panic(fmt.Errorf("backup export codec %q already registered", name))
	}
	backupCodecs.export[name] = codec
	backupCodecs.Unlock()

	return func() {
		backupCodecs.Lock()
		delete(backupCodecs.export, name)
		backupCodecs.Unlock()
	}
}

func RegisterBackupImportCodec(name string, codec BackupImportCodec) func() {
	if name == "" {
		panic("backup import codec name is required")
	}
	if codec.HeaderBytes <= 0 {
		panic(fmt.Errorf("backup import codec %q header size is required", name))
	}
	if codec.Match == nil {
		panic(fmt.Errorf("backup import codec %q matcher is required", name))
	}
	if codec.Decode == nil {
		panic(fmt.Errorf("backup import codec %q decoder is required", name))
	}

	backupCodecs.Lock()
	if _, exists := backupCodecs.imports[name]; exists {
		backupCodecs.Unlock()
		panic(fmt.Errorf("backup import codec %q already registered", name))
	}
	backupCodecs.imports[name] = codec
	backupCodecs.Unlock()

	return func() {
		backupCodecs.Lock()
		delete(backupCodecs.imports, name)
		backupCodecs.Unlock()
	}
}

func ResetBackupCodecsForTest() {
	backupCodecs.Lock()
	backupCodecs.export = map[string]BackupExportCodec{}
	backupCodecs.imports = map[string]BackupImportCodec{}
	backupCodecs.Unlock()
}

func selectedBackupExportCodec(c *gin.Context) (backupExportCodecEntry, bool) {
	for _, entry := range backupExportCodecsSnapshot() {
		if entry.codec.Selected(c) {
			return entry, true
		}
	}
	return backupExportCodecEntry{}, false
}

func backupExportCodecRequested(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	for key, values := range c.Request.URL.Query() {
		normalized := strings.ToLower(strings.TrimSpace(key))
		switch {
		case normalized == "backupcodec", normalized == "backupencryption":
			if hasRequestedBackupCodecValue(values) {
				return true
			}
		case strings.HasPrefix(normalized, "encrypt") && strings.HasSuffix(normalized, "backup"):
			if hasRequestedBackupCodecValue(values) {
				return true
			}
		}
	}
	return false
}

func hasRequestedBackupCodecValue(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "0", "false", "no", "off", "none", "plain":
			continue
		default:
			return true
		}
	}
	return false
}

func maxBackupImportCodecHeaderBytes() int {
	maxSize := 0
	for _, entry := range backupImportCodecsSnapshot() {
		if entry.codec.HeaderBytes > maxSize {
			maxSize = entry.codec.HeaderBytes
		}
	}
	return maxSize
}

func matchingBackupImportCodec(header []byte) (backupImportCodecEntry, bool) {
	for _, entry := range backupImportCodecsSnapshot() {
		if len(header) < entry.codec.HeaderBytes {
			continue
		}
		if entry.codec.Match(header[:entry.codec.HeaderBytes]) {
			return entry, true
		}
	}
	return backupImportCodecEntry{}, false
}

func backupExportCodecsSnapshot() []backupExportCodecEntry {
	backupCodecs.RLock()
	entries := make([]backupExportCodecEntry, 0, len(backupCodecs.export))
	for name, codec := range backupCodecs.export {
		entries = append(entries, backupExportCodecEntry{name: name, codec: codec})
	}
	backupCodecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

func backupImportCodecsSnapshot() []backupImportCodecEntry {
	backupCodecs.RLock()
	entries := make([]backupImportCodecEntry, 0, len(backupCodecs.imports))
	for name, codec := range backupCodecs.imports {
		entries = append(entries, backupImportCodecEntry{name: name, codec: codec})
	}
	backupCodecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

func backupCodecHTTPError(err error, fallbackClass string) (int, string) {
	var codecErr *BackupCodecError
	if errors.As(err, &codecErr) {
		return codecErr.Status, codecErr.Class
	}
	if fallbackClass == "" {
		fallbackClass = "failed"
	}
	return http.StatusInternalServerError, fallbackClass
}
