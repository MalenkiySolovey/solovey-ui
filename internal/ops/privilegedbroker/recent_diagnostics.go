package privilegedbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	RecentDiagnosticSchema     = "solovey-ui/recent-diagnostics/v1"
	RecentDiagnosticRoot       = StandaloneSocketRoot + "/diagnostics"
	RecentDiagnosticPath       = RecentDiagnosticRoot + "/recent.json"
	MaxRecentDiagnosticRecords = 64
	maxRecentDiagnosticBytes   = 64 << 10
)

// RecentDiagnostic is the bounded product-owned projection retained for an
// operator after a fail-closed broker operation. Meaning is supplied by the
// owner-local handler; this neutral record adds no classifications of its own.
type RecentDiagnostic struct {
	Timestamp             int64  `json:"timestamp"`
	Owner                 string `json:"owner"`
	Operation             Verb   `json:"operation"`
	Stage                 string `json:"stage"`
	Reason                string `json:"reason"`
	ErrnoClass            string `json:"errnoClass,omitempty"`
	ProofMethod           string `json:"proofMethod,omitempty"`
	DescriptorCount       int    `json:"descriptorCount,omitempty"`
	SocketDescriptorCount int    `json:"socketDescriptorCount,omitempty"`
	DuplicateAttempts     int    `json:"duplicateAttempts,omitempty"`
	DuplicatedSockets     int    `json:"duplicatedSockets,omitempty"`
	RetryCount            int    `json:"retryCount,omitempty"`
}

type RecentDiagnostics struct {
	Schema  string             `json:"schema"`
	Records []RecentDiagnostic `json:"records"`
}

// RecentDiagnosticRing is a single-writer runtime-only ring. It is neither a
// semantic owner nor a lifecycle service; the privileged broker owns its one
// in-process instance.
type RecentDiagnosticRing struct {
	mu       sync.Mutex
	root     string
	path     string
	ownerUID uint32
	records  []RecentDiagnostic
}

func OpenRecentDiagnosticRing() (*RecentDiagnosticRing, error) {
	uid, supported := diagnosticCurrentUID()
	if !supported || uid != 0 {
		return nil, errors.New("recent diagnostic ring requires root")
	}
	if err := ensureRecentDiagnosticRoot(RecentDiagnosticRoot, uid); err != nil {
		return nil, err
	}
	return openRecentDiagnosticRingAt(RecentDiagnosticRoot, uid)
}

func openRecentDiagnosticRingAt(root string, ownerUID uint32) (*RecentDiagnosticRing, error) {
	if err := validateRecentDiagnosticRoot(root, ownerUID); err != nil {
		return nil, err
	}
	ring := &RecentDiagnosticRing{root: root, path: filepath.Join(root, "recent.json"), ownerUID: ownerUID}
	document, err := readRecentDiagnosticsAt(ring.path, ownerUID)
	if errors.Is(err, os.ErrNotExist) {
		document = RecentDiagnostics{Schema: RecentDiagnosticSchema, Records: []RecentDiagnostic{}}
		if err := ring.write(document); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	ring.records = append([]RecentDiagnostic(nil), document.Records...)
	return ring, nil
}

func (r *RecentDiagnosticRing) Record(event DiagnosticEvent) error {
	if r == nil {
		return errors.New("recent diagnostic ring is unavailable")
	}
	record := RecentDiagnostic{
		Timestamp: event.Timestamp.UTC().UnixMilli(), Owner: event.HandlerOwner, Operation: event.Operation,
		Stage: event.HandlerStage, Reason: event.HandlerReason, ErrnoClass: event.HandlerErrno,
		ProofMethod: event.ProofMethod, DescriptorCount: event.DescriptorCount,
		SocketDescriptorCount: event.SocketDescriptorCount, DuplicateAttempts: event.DuplicateAttempts,
		DuplicatedSockets: event.DuplicatedSockets, RetryCount: event.RetryCount,
	}
	// Untyped broker denials continue through ordinary audit/campaign capture;
	// the recent owner ring contains only already-classified handler evidence.
	if record.Owner == "" && record.Stage == "" && record.Reason == "" {
		return nil
	}
	if err := record.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := append(append([]RecentDiagnostic(nil), r.records...), record)
	if len(next) > MaxRecentDiagnosticRecords {
		next = append([]RecentDiagnostic(nil), next[len(next)-MaxRecentDiagnosticRecords:]...)
	}
	document := RecentDiagnostics{Schema: RecentDiagnosticSchema, Records: next}
	if err := r.write(document); err != nil {
		return err
	}
	r.records = next
	return nil
}

func ReadRecentDiagnostics() (RecentDiagnostics, error) {
	uid, supported := diagnosticCurrentUID()
	if !supported || uid != 0 {
		return RecentDiagnostics{}, errors.New("recent diagnostic evidence requires root")
	}
	if err := validateRecentDiagnosticRoot(RecentDiagnosticRoot, uid); err != nil {
		return RecentDiagnostics{}, err
	}
	return readRecentDiagnosticsAt(RecentDiagnosticPath, uid)
}

func (r *RecentDiagnosticRing) write(document RecentDiagnostics) error {
	if err := document.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(document)
	if err != nil || len(data)+1 > maxRecentDiagnosticBytes {
		return errors.New("recent diagnostic document exceeds its bounded schema")
	}
	temporary, err := os.CreateTemp(r.root, ".recent-*")
	if err != nil {
		return errors.New("recent diagnostic temporary cannot be created")
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	fail := func(cause error) error {
		_ = temporary.Close()
		return cause
	}
	if err := temporary.Chmod(0o600); err != nil {
		return fail(errors.New("recent diagnostic temporary mode cannot be established"))
	}
	info, err := temporary.Stat()
	if err != nil {
		return fail(errors.New("recent diagnostic temporary identity is unavailable"))
	}
	if uid, ok := diagnosticFileUIDFromInfo(info); !ok || uid != r.ownerUID {
		return fail(errors.New("recent diagnostic temporary owner is invalid"))
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		return fail(errors.New("recent diagnostic document cannot be written"))
	}
	if err := temporary.Sync(); err != nil {
		return fail(errors.New("recent diagnostic document cannot be synchronized"))
	}
	if err := temporary.Close(); err != nil {
		return errors.New("recent diagnostic document cannot be closed")
	}
	if err := os.Rename(temporaryName, r.path); err != nil {
		return errors.New("recent diagnostic document cannot be published")
	}
	directory, err := os.Open(r.root)
	if err != nil {
		return errors.New("recent diagnostic root cannot be opened")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("recent diagnostic root cannot be synchronized")
	}
	return nil
}

func validateRecentDiagnosticRoot(root string, ownerUID uint32) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != "diagnostics" {
		return errors.New("recent diagnostic root is invalid")
	}
	for index, candidate := range []string{filepath.Dir(root), root} {
		info, err := os.Lstat(candidate)
		if err != nil {
			return err
		}
		uid, ok := diagnosticFileUIDFromInfo(info)
		if !ok || uid != ownerUID || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("recent diagnostic ancestry is untrusted")
		}
		if index == 1 && info.Mode().Perm() != 0o700 {
			return errors.New("recent diagnostic root mode is invalid")
		}
	}
	return nil
}

func ensureRecentDiagnosticRoot(root string, ownerUID uint32) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != "diagnostics" {
		return errors.New("recent diagnostic root is invalid")
	}
	parent := filepath.Dir(root)
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	uid, ok := diagnosticFileUIDFromInfo(info)
	if !ok || uid != ownerUID || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("recent diagnostic parent is untrusted")
	}
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return errors.New("recent diagnostic root cannot be created")
	}
	return validateRecentDiagnosticRoot(root, ownerUID)
}

func readRecentDiagnosticsAt(path string, ownerUID uint32) (RecentDiagnostics, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return RecentDiagnostics{}, err
	}
	uid, ok := diagnosticFileUIDFromInfo(info)
	if !ok || uid != ownerUID || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > maxRecentDiagnosticBytes {
		return RecentDiagnostics{}, errors.New("recent diagnostic document is untrusted or unbounded")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RecentDiagnostics{}, errors.New("recent diagnostic document cannot be read")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document RecentDiagnostics
	if err := decoder.Decode(&document); err != nil {
		return RecentDiagnostics{}, errors.New("recent diagnostic document is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return RecentDiagnostics{}, errors.New("recent diagnostic document has trailing data")
	}
	if err := document.validate(); err != nil {
		return RecentDiagnostics{}, err
	}
	return document, nil
}

func (d RecentDiagnostics) validate() error {
	if d.Schema != RecentDiagnosticSchema || d.Records == nil || len(d.Records) > MaxRecentDiagnosticRecords {
		return errors.New("recent diagnostic document has an invalid bounded envelope")
	}
	for index, record := range d.Records {
		if err := record.validate(); err != nil {
			return fmt.Errorf("recent diagnostic record %d is invalid", index)
		}
	}
	return nil
}

func (r RecentDiagnostic) validate() error {
	if r.Timestamp <= 0 || sanitizeAuditVerb(r.Operation) != r.Operation || r.Operation == "" ||
		!validDiagnosticToken(r.Owner, true) || !validDiagnosticToken(r.Stage, true) ||
		!validDiagnosticToken(r.Reason, true) || !validDiagnosticToken(r.ErrnoClass, false) ||
		!validDiagnosticToken(r.ProofMethod, false) {
		return errors.New("recent diagnostic record contains an invalid identity")
	}
	counters := []int{r.DescriptorCount, r.SocketDescriptorCount, r.DuplicateAttempts, r.DuplicatedSockets, r.RetryCount}
	for _, counter := range counters {
		if counter < 0 || counter > 1<<20 {
			return errors.New("recent diagnostic counter is invalid")
		}
	}
	return nil
}

func validDiagnosticToken(value string, required bool) bool {
	if value == "" {
		return !required
	}
	return sanitizeHandlerDiagnostic(value) == value
}
