package backupcodec

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type ExportContext struct {
	Gin     *gin.Context
	Exclude string
	Plain   []byte
}

type ExportResult struct {
	Payload       []byte
	Encrypted     bool
	AuditEvent    string
	AuditSeverity string
	AuditDetails  map[string]any
	PlainBytes    int64
	PayloadBytes  int64
}

type ExportStreamContext struct {
	Gin         *gin.Context
	Exclude     string
	Plain       io.ReadSeeker
	Destination io.Writer
}

type ExportCodec struct {
	Selected     func(*gin.Context) bool
	Preflight    func(*gin.Context) error
	Encode       func(ExportContext) (ExportResult, error)
	EncodeStream func(ExportStreamContext) (ExportResult, error)
}

type ImportContext struct {
	Gin     *gin.Context
	Payload []byte
}

type ImportStreamContext struct {
	Gin         *gin.Context
	Source      io.ReadSeeker
	Destination io.WriteSeeker
	MaxBytes    int64
}

type ImportCodec struct {
	HeaderBytes       int
	PassphraseFields  []string
	Match             func([]byte) bool
	Decode            func(ImportContext) ([]byte, error)
	DecodeStream      func(ImportStreamContext) error
	FailureAuditEvent string
}

type Error struct {
	Status int
	Class  string
	Err    error
}

func NewError(status int, class string, err error) error {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if class == "" {
		class = "failed"
	}
	return &Error{Status: status, Class: class, Err: err}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Class, e.Err)
	}
	return e.Class
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type exportEntry struct {
	name  string
	codec ExportCodec
}

type importEntry struct {
	name  string
	codec ImportCodec
}

var codecs = struct {
	sync.RWMutex
	exports map[string]ExportCodec
	imports map[string]ImportCodec
}{
	exports: map[string]ExportCodec{},
	imports: map[string]ImportCodec{},
}

const (
	maxCodecs                 = 64
	maxImportPassphraseFields = 8
	maxImportFieldNameBytes   = 64
)

func RegisterExport(name string, codec ExportCodec) func() {
	if name == "" || codec.Selected == nil || codec.Encode == nil && codec.EncodeStream == nil {
		panic(fmt.Errorf("backup export codec %q is incomplete", name))
	}
	codecs.Lock()
	defer codecs.Unlock()
	if _, exists := codecs.exports[name]; exists {
		panic(fmt.Errorf("backup export codec %q already registered", name))
	}
	if len(codecs.exports) >= maxCodecs {
		panic("backup export codec capacity exceeded")
	}
	codecs.exports[name] = codec
	return unregisterExport(name)
}

func RegisterImport(name string, codec ImportCodec) func() {
	if name == "" || codec.HeaderBytes <= 0 || codec.Match == nil || codec.Decode == nil && codec.DecodeStream == nil {
		panic(fmt.Errorf("backup import codec %q is incomplete", name))
	}
	codec.PassphraseFields = normalizedPassphraseFields(name, codec.PassphraseFields)
	codecs.Lock()
	defer codecs.Unlock()
	if _, exists := codecs.imports[name]; exists {
		panic(fmt.Errorf("backup import codec %q already registered", name))
	}
	if len(codecs.imports) >= maxCodecs {
		panic("backup import codec capacity exceeded")
	}
	codecs.imports[name] = codec
	return unregisterImport(name)
}

func unregisterExport(name string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			codecs.Lock()
			delete(codecs.exports, name)
			codecs.Unlock()
		})
	}
}

func unregisterImport(name string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			codecs.Lock()
			delete(codecs.imports, name)
			codecs.Unlock()
		})
	}
}

func ResetForTest() {
	codecs.Lock()
	codecs.exports = map[string]ExportCodec{}
	codecs.imports = map[string]ImportCodec{}
	codecs.Unlock()
}

func SelectedExport(c *gin.Context) (string, ExportCodec, bool) {
	for _, entry := range exportSnapshot() {
		if entry.codec.Selected(c) {
			return entry.name, entry.codec, true
		}
	}
	return "", ExportCodec{}, false
}

func ExportRequested(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	for key, values := range c.Request.URL.Query() {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if (normalized == "backupcodec" || normalized == "backupencryption" ||
			(strings.HasPrefix(normalized, "encrypt") && strings.HasSuffix(normalized, "backup"))) &&
			hasRequestedValue(values) {
			return true
		}
	}
	return false
}

func MaxImportHeaderBytes() int {
	maxSize := 0
	for _, entry := range importSnapshot() {
		if entry.codec.HeaderBytes > maxSize {
			maxSize = entry.codec.HeaderBytes
		}
	}
	return maxSize
}

func MatchingImport(header []byte) (string, ImportCodec, bool) {
	for _, entry := range importSnapshot() {
		if len(header) >= entry.codec.HeaderBytes && entry.codec.Match(header[:entry.codec.HeaderBytes]) {
			return entry.name, entry.codec, true
		}
	}
	return "", ImportCodec{}, false
}

// ImportPassphraseFields returns the bounded union of component-owned secret
// fields accepted by the currently registered import codecs. The core import
// handler remains the authority for multipart limits; codecs only contribute
// the names of passphrase fields they consume.
func ImportPassphraseFields() []string {
	unique := map[string]struct{}{}
	for _, entry := range importSnapshot() {
		for _, field := range entry.codec.PassphraseFields {
			unique[field] = struct{}{}
		}
	}
	fields := make([]string, 0, len(unique))
	for field := range unique {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func HTTPError(err error, fallbackClass string) (int, string) {
	var codecErr *Error
	if errors.As(err, &codecErr) {
		return codecErr.Status, codecErr.Class
	}
	if fallbackClass == "" {
		fallbackClass = "failed"
	}
	return http.StatusInternalServerError, fallbackClass
}

func hasRequestedValue(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "0", "false", "no", "off", "none", "plain":
		default:
			return true
		}
	}
	return false
}

func exportSnapshot() []exportEntry {
	codecs.RLock()
	entries := make([]exportEntry, 0, len(codecs.exports))
	for name, codec := range codecs.exports {
		entries = append(entries, exportEntry{name: name, codec: codec})
	}
	codecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func importSnapshot() []importEntry {
	codecs.RLock()
	entries := make([]importEntry, 0, len(codecs.imports))
	for name, codec := range codecs.imports {
		entries = append(entries, importEntry{name: name, codec: codec})
	}
	codecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func normalizedPassphraseFields(codecName string, fields []string) []string {
	if len(fields) > maxImportPassphraseFields {
		panic(fmt.Errorf("backup import codec %q declares too many passphrase fields", codecName))
	}
	normalized := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if !validImportFieldName(field) {
			panic(fmt.Errorf("backup import codec %q has invalid passphrase field %q", codecName, field))
		}
		if _, exists := seen[field]; exists {
			panic(fmt.Errorf("backup import codec %q repeats passphrase field %q", codecName, field))
		}
		seen[field] = struct{}{}
		normalized = append(normalized, field)
	}
	sort.Strings(normalized)
	return normalized
}

func validImportFieldName(field string) bool {
	if len(field) == 0 || len(field) > maxImportFieldNameBytes {
		return false
	}
	for index, value := range []byte(field) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || index > 0 && value >= '0' && value <= '9' {
			continue
		}
		return false
	}
	return true
}
