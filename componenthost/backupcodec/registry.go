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
	token uint64
}

type importEntry struct {
	name  string
	codec ImportCodec
	token uint64
}

var codecs = struct {
	sync.RWMutex
	exports   map[string]exportEntry
	imports   map[string]importEntry
	nextToken uint64
}{
	exports: map[string]exportEntry{},
	imports: map[string]importEntry{},
}

const (
	maxCodecs                 = 64
	maxImportPassphraseFields = 8
	maxImportFieldNameBytes   = 64
	maxImportHeaderBytes      = 4096
	maxImportFieldsTotal      = 64
)

func RegisterExport(name string, codec ExportCodec) (func(), error) {
	if !validCodecName(name) || codec.Selected == nil || codec.Encode == nil && codec.EncodeStream == nil {
		return nil, fmt.Errorf("backup export codec %q is incomplete", name)
	}
	codecs.Lock()
	defer codecs.Unlock()
	if _, exists := codecs.exports[name]; exists {
		return nil, fmt.Errorf("backup export codec %q already registered", name)
	}
	if len(codecs.exports) >= maxCodecs {
		return nil, errors.New("backup export codec capacity exceeded")
	}
	codecs.nextToken++
	token := codecs.nextToken
	codecs.exports[name] = exportEntry{name: name, codec: codec, token: token}
	return unregisterExport(name, token), nil
}

func RegisterImport(name string, codec ImportCodec) (func(), error) {
	if !validCodecName(name) || codec.HeaderBytes <= 0 || codec.HeaderBytes > maxImportHeaderBytes || codec.Match == nil || codec.Decode == nil && codec.DecodeStream == nil {
		return nil, fmt.Errorf("backup import codec %q is incomplete", name)
	}
	fields, err := normalizedPassphraseFields(name, codec.PassphraseFields)
	if err != nil {
		return nil, err
	}
	codec.PassphraseFields = fields
	codecs.Lock()
	defer codecs.Unlock()
	if _, exists := codecs.imports[name]; exists {
		return nil, fmt.Errorf("backup import codec %q already registered", name)
	}
	if len(codecs.imports) >= maxCodecs {
		return nil, errors.New("backup import codec capacity exceeded")
	}
	unique := make(map[string]struct{})
	for _, entry := range codecs.imports {
		for _, field := range entry.codec.PassphraseFields {
			unique[field] = struct{}{}
		}
	}
	for _, field := range codec.PassphraseFields {
		unique[field] = struct{}{}
	}
	if len(unique) > maxImportFieldsTotal {
		return nil, errors.New("backup import passphrase field capacity exceeded")
	}
	codecs.nextToken++
	token := codecs.nextToken
	codecs.imports[name] = importEntry{name: name, codec: codec, token: token}
	return unregisterImport(name, token), nil
}

func unregisterExport(name string, token uint64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			codecs.Lock()
			if current, ok := codecs.exports[name]; ok && current.token == token {
				delete(codecs.exports, name)
			}
			codecs.Unlock()
		})
	}
}

func unregisterImport(name string, token uint64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			codecs.Lock()
			if current, ok := codecs.imports[name]; ok && current.token == token {
				delete(codecs.imports, name)
			}
			codecs.Unlock()
		})
	}
}

func SelectedExport(c *gin.Context) (string, ExportCodec, bool, error) {
	var selected *exportEntry
	for _, entry := range exportSnapshot() {
		matches, panicked := safeExportSelection(entry.codec, c)
		if panicked {
			return "", ExportCodec{}, false, NewError(http.StatusInternalServerError, "backup_codec_failed", nil)
		}
		if matches {
			if selected != nil {
				return "", ExportCodec{}, false, NewError(http.StatusBadRequest, "backup_codec_ambiguous", nil)
			}
			copy := entry
			selected = &copy
		}
	}
	if selected == nil {
		return "", ExportCodec{}, false, nil
	}
	return selected.name, selected.codec, true, nil
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

func MatchingImport(header []byte) (string, ImportCodec, bool, error) {
	var selected *importEntry
	for _, entry := range importSnapshot() {
		matches, panicked := safeImportMatch(entry.codec, header)
		if panicked {
			return "", ImportCodec{}, false, NewError(http.StatusInternalServerError, "backup_codec_failed", nil)
		}
		if matches {
			if selected != nil {
				return "", ImportCodec{}, false, NewError(http.StatusBadRequest, "backup_codec_ambiguous", nil)
			}
			copy := entry
			selected = &copy
		}
	}
	if selected == nil {
		return "", ImportCodec{}, false, nil
	}
	return selected.name, selected.codec, true, nil
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
		status, class := codecErr.Status, codecErr.Class
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		if !validErrorClass(class) {
			class = "failed"
		}
		return status, class
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
	for _, entry := range codecs.exports {
		entries = append(entries, entry)
	}
	codecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func importSnapshot() []importEntry {
	codecs.RLock()
	entries := make([]importEntry, 0, len(codecs.imports))
	for _, entry := range codecs.imports {
		entries = append(entries, entry)
	}
	codecs.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func safeExportSelection(codec ExportCodec, c *gin.Context) (selected, panicked bool) {
	defer func() {
		if recover() != nil {
			selected, panicked = false, true
		}
	}()
	return codec.Selected(c), false
}

func safeImportMatch(codec ImportCodec, header []byte) (matched, panicked bool) {
	if len(header) < codec.HeaderBytes {
		return false, false
	}
	defer func() {
		if recover() != nil {
			matched, panicked = false, true
		}
	}()
	return codec.Match(header[:codec.HeaderBytes]), false
}

func validCodecName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validErrorClass(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func normalizedPassphraseFields(codecName string, fields []string) ([]string, error) {
	if len(fields) > maxImportPassphraseFields {
		return nil, fmt.Errorf("backup import codec %q declares too many passphrase fields", codecName)
	}
	normalized := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if !validImportFieldName(field) {
			return nil, fmt.Errorf("backup import codec %q has invalid passphrase field %q", codecName, field)
		}
		if _, exists := seen[field]; exists {
			return nil, fmt.Errorf("backup import codec %q repeats passphrase field %q", codecName, field)
		}
		seen[field] = struct{}{}
		normalized = append(normalized, field)
	}
	sort.Strings(normalized)
	return normalized, nil
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
