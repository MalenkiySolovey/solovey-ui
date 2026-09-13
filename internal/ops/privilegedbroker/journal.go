package privilegedbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	journalFileName       = "receipts-v1.jsonl"
	maxJournalBytes       = 8 << 20
	maxJournalFileBytes   = maxJournalBytes * 2
	maxJournalRows        = 4096
	targetJournalRows     = maxJournalRows / 2
	maxJournalRowBytes    = MaxResponseBytes * 2
	maxJournalRowLineSize = maxJournalRowBytes + 1
)

type completedMutationReference struct {
	OperationID   string `json:"operationId"`
	Verb          Verb   `json:"verb"`
	FenceResource string `json:"fenceResource"`
}

func (r completedMutationReference) valid() bool {
	return safeIdentifier(r.OperationID) && validVerb(string(r.Verb)) && safeIdentifier(r.FenceResource)
}

func completedMutationKey(operationID string, verb Verb, fenceResource string) string {
	return operationID + "\x00" + string(verb) + "\x00" + fenceResource
}

type journalAuthorityState struct {
	Sequence       uint64            `json:"sequence"`
	PreviousDigest string            `json:"previousDigest,omitempty"`
	Fences         map[string]uint64 `json:"fences,omitempty"`
}

type journalRow struct {
	Schema             int                         `json:"schema"`
	Phase              string                      `json:"phase"`
	IdempotencyKey     string                      `json:"idempotencyKey,omitempty"`
	RequestDigest      string                      `json:"requestDigest,omitempty"`
	FenceResource      string                      `json:"fenceResource,omitempty"`
	FenceSequence      uint64                      `json:"fenceSequence,omitempty"`
	StartedAt          int64                       `json:"startedAt,omitempty"`
	Response           *Response                   `json:"response,omitempty"`
	Receipt            *Receipt                    `json:"receipt,omitempty"`
	Payload            json.RawMessage             `json:"payload,omitempty"`
	Authority          *journalAuthorityState      `json:"authority,omitempty"`
	RetainUntilRelease *bool                       `json:"retainUntilRelease,omitempty"`
	Releases           *completedMutationReference `json:"releases,omitempty"`
}

type replayRecord struct {
	requestDigest string
	response      Response
}

// CompletionPolicy is broker-local lifecycle metadata for a completed typed
// mutation. It never grants a generic persistence or execution capability.
type CompletionPolicy struct {
	RetainUntilRelease bool
	ReleasesVerb       Verb
}

type Journal interface {
	Begin(Request, PeerIdentity, string, time.Time) (*Response, *Receipt, error)
	Commit(Request, *Receipt, Response, CompletionPolicy, time.Time) (Response, error)
	Unresolved() []Receipt
}

// CompletedMutationAuthority exposes only an already committed typed broker
// result. It lets a handler recover server-owned rollback authority without
// accepting a replacement checkpoint from the panel process.
type CompletedMutationAuthority interface {
	CompletedMutation(operationID string, verb Verb, fenceResource string) (Response, error)
}

type journalFile interface {
	io.Reader
	io.Writer
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
}

type journalFilesystem struct {
	lstat         func(string) (os.FileInfo, error)
	mkdir         func(string, os.FileMode) error
	open          func(string) (journalFile, error)
	openFile      func(string, int, os.FileMode) (journalFile, error)
	remove        func(string) error
	rename        func(string, string) error
	truncate      func(string, int64) error
	syncDirectory func(string) error
}

func realJournalFilesystem() journalFilesystem {
	return journalFilesystem{
		lstat: os.Lstat,
		mkdir: os.Mkdir,
		open: func(path string) (journalFile, error) {
			return os.Open(path)
		},
		openFile: func(path string, flag int, mode os.FileMode) (journalFile, error) {
			return os.OpenFile(path, flag, mode)
		},
		remove:        os.Remove,
		rename:        os.Rename,
		truncate:      os.Truncate,
		syncDirectory: syncJournalDirectory,
	}
}

func (f journalFilesystem) valid() bool {
	return f.lstat != nil && f.mkdir != nil && f.open != nil && f.openFile != nil && f.remove != nil && f.rename != nil && f.truncate != nil && f.syncDirectory != nil
}

type journalIndexes struct {
	sequence      uint64
	previous      string
	replays       map[string]replayRecord
	fences        map[string]uint64
	unresolved    map[string]Receipt
	completed     map[string]journalRow
	liveCompleted map[string]journalRow
	released      map[string]struct{}
}

func newJournalIndexes() journalIndexes {
	return journalIndexes{
		replays:       make(map[string]replayRecord),
		fences:        make(map[string]uint64),
		unresolved:    make(map[string]Receipt),
		completed:     make(map[string]journalRow),
		liveCompleted: make(map[string]journalRow),
		released:      make(map[string]struct{}),
	}
}

type FileJournal struct {
	mu            sync.Mutex
	root          string
	path          string
	bootID        string
	sequence      uint64
	previous      string
	replays       map[string]replayRecord
	fences        map[string]uint64
	unresolved    map[string]Receipt
	completed     map[string]journalRow
	liveCompleted map[string]journalRow
	released      map[string]struct{}
	rows          []journalRow
	fileBytes     int64
	fs            journalFilesystem
}

func OpenFileJournal(root, bootID string) (*FileJournal, error) {
	return openFileJournal(root, bootID, realJournalFilesystem(), true)
}

func openFileJournal(root, bootID string, fs journalFilesystem, validateOwnership bool) (*FileJournal, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || filepath.Base(root) != "solovey-ui-broker" {
		return nil, errors.New("broker journal root is not the fixed dedicated directory")
	}
	if !fs.valid() {
		return nil, errors.New("broker journal filesystem is incomplete")
	}
	if err := ensurePrivateDirectory(fs, root, validateOwnership); err != nil {
		return nil, err
	}
	journal := &FileJournal{root: root, path: filepath.Join(root, journalFileName), bootID: bootID, fs: fs}
	journal.installRows(nil, newJournalIndexes(), 0)
	if err := journal.load(validateOwnership); err != nil {
		return nil, err
	}
	return journal, nil
}

func ensurePrivateDirectory(fs journalFilesystem, root string, validateOwnership bool) error {
	info, err := fs.lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := fs.mkdir(root, 0o700); err != nil {
			return fmt.Errorf("create broker journal root: %w", err)
		}
		info, err = fs.lstat(root)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("broker journal root ownership mode is unsafe")
	}
	if validateOwnership {
		if err := validateOwnedByRoot(root); err != nil {
			return err
		}
	}
	// Re-sync even an existing root. A previous interrupted/failed open may
	// have created the directory but returned before its parent became durable.
	if err := fs.syncDirectory(filepath.Dir(root)); err != nil {
		return fmt.Errorf("sync broker journal parent: %w", err)
	}
	return nil
}

type loadedJournal struct {
	rows       []journalRow
	indexes    journalIndexes
	fileBytes  int64
	tailOffset int64
}

func (j *FileJournal) load(validateOwnership bool) error {
	if !j.fs.valid() {
		j.fs = realJournalFilesystem()
	}
	temporary := j.path + ".new"
	mainExists, err := journalPathExists(j.fs, j.path)
	if err != nil {
		return err
	}
	temporaryExists, err := journalPathExists(j.fs, temporary)
	if err != nil {
		return err
	}
	if mainExists {
		loaded, err := j.readJournalFile(j.path, validateOwnership, true)
		if err != nil {
			return err
		}
		if temporaryExists {
			if err := j.removeCompactionTemporary(temporary); err != nil {
				return err
			}
		}
		if loaded.tailOffset >= 0 {
			if err := j.recoverInterruptedTail(loaded.tailOffset); err != nil {
				return err
			}
			loaded.fileBytes = loaded.tailOffset
		}
		j.installRows(loaded.rows, loaded.indexes, loaded.fileBytes)
		if len(j.rows) > maxJournalRows || j.fileBytes > maxJournalBytes {
			return j.compact(j.rows, -1, 0, 0)
		}
		return nil
	}
	if temporaryExists {
		staged, stageErr := j.readJournalFile(temporary, validateOwnership, false)
		if stageErr != nil {
			return fmt.Errorf("broker compaction recovery is invalid: %w", stageErr)
		}
		if err := j.fs.rename(temporary, j.path); err != nil {
			return fmt.Errorf("publish recovered broker compaction: %w", err)
		}
		if err := j.fs.syncDirectory(j.root); err != nil {
			return fmt.Errorf("sync recovered broker compaction: %w", err)
		}
		j.installRows(staged.rows, staged.indexes, staged.fileBytes)
		if len(j.rows) > maxJournalRows || j.fileBytes > maxJournalBytes {
			return j.compact(j.rows, -1, 0, 0)
		}
	}
	return nil
}

func journalPathExists(fs journalFilesystem, path string) (bool, error) {
	_, err := fs.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (j *FileJournal) removeCompactionTemporary(path string) error {
	if err := j.fs.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale broker compaction: %w", err)
	}
	if err := j.fs.syncDirectory(j.root); err != nil {
		return fmt.Errorf("sync stale broker compaction removal: %w", err)
	}
	return nil
}

func (j *FileJournal) readJournalFile(path string, validateOwnership, recoverTail bool) (loadedJournal, error) {
	file, err := j.fs.open(path)
	if err != nil {
		return loadedJournal{}, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || validateOwnership && info.Mode().Perm()&0o077 != 0 || info.Size() > maxJournalFileBytes {
		_ = file.Close()
		return loadedJournal{}, errors.New("broker receipt journal is unsafe")
	}
	if validateOwnership {
		if err := validateOwnedByRoot(path); err != nil {
			_ = file.Close()
			return loadedJournal{}, err
		}
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxJournalFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) > maxJournalFileBytes {
		return loadedJournal{}, errors.Join(readErr, closeErr, errors.New("broker receipt journal read failed"))
	}
	loaded, err := parseJournal(data, recoverTail)
	if err != nil {
		return loadedJournal{}, err
	}
	return loaded, nil
}

func parseJournal(data []byte, recoverTail bool) (loadedJournal, error) {
	prefixLength := len(data)
	tailOffset := int64(-1)
	if len(data) != 0 && data[len(data)-1] != '\n' {
		if !recoverTail {
			return loadedJournal{}, errors.New("broker compaction contains an unterminated record")
		}
		lastNewline := bytes.LastIndexByte(data, '\n')
		prefixLength = lastNewline + 1
		tailOffset = int64(prefixLength)
	}
	rows := make([]journalRow, 0, bytes.Count(data[:prefixLength], []byte{'\n'}))
	lines := bytes.Split(data[:prefixLength], []byte{'\n'})
	for index, line := range lines {
		if len(line) == 0 {
			if index != len(lines)-1 {
				return loadedJournal{}, errors.New("broker receipt journal contains an empty committed record")
			}
			continue
		}
		if len(line) > maxJournalRowBytes {
			return loadedJournal{}, errors.New("broker receipt journal row exceeds its bounded contract")
		}
		var row journalRow
		if err := decodeStrict(line, &row); err != nil || validateJournalRow(row) != nil {
			return loadedJournal{}, errors.New("broker receipt journal contains an invalid committed record")
		}
		rows = append(rows, row)
	}
	indexes, err := buildJournalIndexes(rows)
	if err != nil {
		return loadedJournal{}, err
	}
	return loadedJournal{rows: rows, indexes: indexes, fileBytes: int64(prefixLength), tailOffset: tailOffset}, nil
}

func (j *FileJournal) recoverInterruptedTail(offset int64) error {
	if err := j.fs.truncate(j.path, offset); err != nil {
		return fmt.Errorf("truncate interrupted broker journal tail: %w", err)
	}
	file, err := j.fs.openFile(j.path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open recovered broker journal: %w", err)
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("sync recovered broker journal: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func validateJournalRow(row journalRow) error {
	if row.Schema != 1 {
		return errors.New("broker journal schema is invalid")
	}
	if row.Phase == "authority" {
		if row.Authority == nil || row.IdempotencyKey != "" || row.RequestDigest != "" || row.FenceResource != "" || row.FenceSequence != 0 ||
			row.StartedAt != 0 || row.Response != nil || row.Receipt != nil || len(row.Payload) != 0 || row.RetainUntilRelease != nil || row.Releases != nil {
			return errors.New("broker journal authority snapshot is invalid")
		}
		if row.Authority.Sequence == 0 && row.Authority.PreviousDigest != "" || row.Authority.Sequence != 0 && !digestPattern.MatchString(row.Authority.PreviousDigest) {
			return errors.New("broker journal chain authority is invalid")
		}
		for resource, sequence := range row.Authority.Fences {
			if !safeIdentifier(resource) || sequence == 0 {
				return errors.New("broker journal fence authority is invalid")
			}
		}
		return nil
	}
	if row.IdempotencyKey == "" || !digestPattern.MatchString(row.RequestDigest) || row.Receipt == nil ||
		row.Receipt.IdempotencyKey != row.IdempotencyKey || row.Receipt.FenceResource != row.FenceResource || row.Receipt.FenceSequence != row.FenceSequence ||
		!digestPattern.MatchString(row.Receipt.ReceiptDigest) || row.Receipt.ReceiptDigest != receiptDigest(*row.Receipt) {
		return errors.New("broker journal receipt is invalid")
	}
	switch row.Phase {
	case "active":
		if row.Response != nil || len(row.Payload) != 0 || row.Receipt.Outcome != "active" || row.Receipt.CompletedAt != 0 || row.Receipt.ResponseDigest != "" ||
			row.Authority != nil || row.RetainUntilRelease != nil || row.Releases != nil {
			return errors.New("broker active journal row is invalid")
		}
	case "complete":
		if row.Response == nil || row.Response.Receipt == nil || *row.Response.Receipt != *row.Receipt ||
			row.Response.RequestID != row.Receipt.RequestID || row.Response.OperationID != row.Receipt.OperationID || row.Response.Verb != row.Receipt.Verb ||
			len(row.Response.Payload) != 0 || len(row.Payload) > MaxResponseBytes || row.Authority != nil ||
			(len(row.Payload) == 0 && row.Response.PayloadDigest != "") || (len(row.Payload) != 0 && row.Response.PayloadDigest != Digest(row.Payload)) ||
			(row.Response.OK && row.Receipt.Outcome != "succeeded") || (!row.Response.OK && row.Receipt.Outcome != "failed") || row.Receipt.CompletedAt == 0 {
			return errors.New("broker completed journal row is invalid")
		}
		if !row.Response.OK && (row.RetainUntilRelease != nil && *row.RetainUntilRelease || row.Releases != nil) {
			return errors.New("failed broker mutation carries completion lifecycle authority")
		}
		if row.Releases != nil && (!row.Releases.valid() || row.Releases.OperationID != row.Receipt.OperationID || row.Releases.FenceResource != row.Receipt.FenceResource) {
			return errors.New("broker completed release authority is invalid")
		}
		responseAuthority := *row.Response
		responseAuthority.Receipt = nil
		responseAuthority.Payload = nil
		encoded, err := json.Marshal(responseAuthority)
		if err != nil || row.Receipt.ResponseDigest != Digest(append(encoded, row.Payload...)) {
			return errors.New("broker completed response authority is invalid")
		}
	default:
		return errors.New("broker journal phase is invalid")
	}
	return nil
}

func buildJournalIndexes(rows []journalRow) (journalIndexes, error) {
	indexes := newJournalIndexes()
	stateSequence := uint64(0)
	hasState := false
	lastSequence := uint64(0)
	for index, row := range rows {
		if err := validateJournalRow(row); err != nil {
			return journalIndexes{}, err
		}
		if row.Phase == "authority" {
			if index != 0 || hasState {
				return journalIndexes{}, errors.New("broker journal authority snapshot is misplaced")
			}
			hasState = true
			stateSequence = row.Authority.Sequence
			indexes.sequence = row.Authority.Sequence
			indexes.previous = row.Authority.PreviousDigest
			for resource, sequence := range row.Authority.Fences {
				indexes.fences[resource] = sequence
			}
			continue
		}
		if row.Receipt.Sequence < lastSequence {
			return journalIndexes{}, errors.New("broker journal receipt sequence is not monotonic")
		}
		lastSequence = row.Receipt.Sequence
		if !hasState || row.Receipt.Sequence > stateSequence {
			if row.Receipt.Sequence > indexes.sequence {
				indexes.sequence = row.Receipt.Sequence
			}
			indexes.previous = row.Receipt.ReceiptDigest
		}
		if row.FenceSequence > indexes.fences[row.FenceResource] {
			indexes.fences[row.FenceResource] = row.FenceSequence
		}
		switch row.Phase {
		case "active":
			indexes.unresolved[row.IdempotencyKey] = *row.Receipt
		case "complete":
			response := *row.Response
			response.Payload = append(json.RawMessage(nil), row.Payload...)
			indexes.replays[row.IdempotencyKey] = replayRecord{requestDigest: row.RequestDigest, response: response}
			delete(indexes.unresolved, row.IdempotencyKey)
			if row.Response.OK {
				key := completedMutationKey(row.Receipt.OperationID, row.Receipt.Verb, row.Receipt.FenceResource)
				delete(indexes.released, key)
				indexes.completed[key] = row
				if journalRowRetained(row) {
					indexes.liveCompleted[key] = row
				}
			}
			if row.Response.OK && row.Releases != nil {
				key := completedMutationKey(row.Releases.OperationID, row.Releases.Verb, row.Releases.FenceResource)
				indexes.released[key] = struct{}{}
				delete(indexes.completed, key)
				delete(indexes.liveCompleted, key)
			}
		}
	}
	return indexes, nil
}

func journalRowRetained(row journalRow) bool {
	if row.RetainUntilRelease != nil {
		return *row.RetainUntilRelease
	}
	// Before the retention field existed, SSH stage results were the one
	// completed-mutation authority consumed outside replay history.
	return row.Receipt != nil && row.Receipt.Verb == VerbSSHStage && row.Response != nil && row.Response.OK
}

func (j *FileJournal) installRows(rows []journalRow, indexes journalIndexes, fileBytes int64) {
	j.rows = append([]journalRow(nil), rows...)
	j.sequence = indexes.sequence
	j.previous = indexes.previous
	j.replays = indexes.replays
	j.fences = indexes.fences
	j.unresolved = indexes.unresolved
	j.completed = indexes.completed
	j.liveCompleted = indexes.liveCompleted
	j.released = indexes.released
	j.fileBytes = fileBytes
}

func (j *FileJournal) Begin(request Request, peer PeerIdentity, requestDigest string, now time.Time) (*Response, *Receipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if replay, ok := j.replays[request.IdempotencyKey]; ok {
		if replay.requestDigest != requestDigest {
			return nil, nil, Failure(CodeIdempotency, "broker idempotency key conflicts with a prior request")
		}
		response := replay.response
		response.Replay = true
		return &response, response.Receipt, nil
	}
	if _, unresolved := j.unresolved[request.IdempotencyKey]; unresolved {
		return nil, nil, Failure(CodeRecoveryRequired, "broker operation requires explicit recovery")
	}
	if len(j.unresolved) != 0 {
		return nil, nil, Failure(CodeRecoveryRequired, "broker has unresolved mutation authority")
	}
	if maximum := j.fences[request.Fence.Resource]; request.Fence.Sequence <= maximum {
		return nil, nil, Failure(CodeFence, "broker fencing sequence is stale")
	}
	j.sequence++
	receipt := &Receipt{Sequence: j.sequence, RequestID: request.RequestID, OperationID: request.OperationID,
		IdempotencyKey: request.IdempotencyKey, Verb: request.Verb, FenceResource: request.Fence.Resource,
		FenceSequence: request.Fence.Sequence, FenceTokenDigest: Digest([]byte(request.Fence.Token)),
		PayloadDigest: request.PayloadDigest, PeerRevision: peer.Revision, BrokerBootID: j.bootID,
		PreviousDigest: j.previous, Outcome: "active", StartedAt: now.UnixMilli()}
	receipt.ReceiptDigest = receiptDigest(*receipt)
	activeReceipt := *receipt
	row := journalRow{Schema: 1, Phase: "active", IdempotencyKey: request.IdempotencyKey,
		RequestDigest: requestDigest, FenceResource: request.Fence.Resource, FenceSequence: request.Fence.Sequence,
		StartedAt: receipt.StartedAt, Receipt: &activeReceipt}
	if err := j.append(row, maxJournalRowLineSize, 1); err != nil {
		return nil, nil, errors.Join(err, j.reloadAfterWriteFailure())
	}
	return nil, receipt, nil
}

func (j *FileJournal) Commit(request Request, receipt *Receipt, response Response, policy CompletionPolicy, now time.Time) (Response, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if receipt == nil {
		return response, errors.New("broker active receipt is required")
	}
	if policy.ReleasesVerb != "" && !validVerb(string(policy.ReleasesVerb)) {
		return response, errors.New("broker completion release verb is invalid")
	}
	responseCopy := response
	payload := append(json.RawMessage(nil), response.Payload...)
	responseCopy.Payload = nil
	encoded, err := json.Marshal(responseCopy)
	if err != nil {
		return response, err
	}
	receipt.Outcome = "failed"
	if response.OK {
		receipt.Outcome = "succeeded"
	}
	receipt.CompletedAt = now.UnixMilli()
	receipt.ResponseDigest = Digest(append(encoded, payload...))
	receipt.PreviousDigest = j.previous
	receipt.ReceiptDigest = receiptDigest(*receipt)
	response.Receipt = receipt
	responseCopy.Receipt = receipt
	requestDigest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
	row := journalRow{Schema: 1, Phase: "complete", IdempotencyKey: request.IdempotencyKey,
		RequestDigest: requestDigest, FenceResource: request.Fence.Resource, FenceSequence: request.Fence.Sequence,
		StartedAt: receipt.StartedAt, Response: &responseCopy, Receipt: receipt, Payload: payload}
	retain := policy.RetainUntilRelease && response.OK
	row.RetainUntilRelease = &retain
	if response.OK && policy.ReleasesVerb != "" {
		row.Releases = &completedMutationReference{OperationID: request.OperationID, Verb: policy.ReleasesVerb, FenceResource: request.Fence.Resource}
	}
	if err := j.append(row, 0, 0); err != nil {
		return response, errors.Join(err, j.reloadAfterWriteFailure())
	}
	return response, nil
}

func (j *FileJournal) Unresolved() []Receipt {
	j.mu.Lock()
	defer j.mu.Unlock()
	result := make([]Receipt, 0, len(j.unresolved))
	for _, receipt := range j.unresolved {
		result = append(result, receipt)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Sequence < result[right].Sequence })
	return result
}

func (j *FileJournal) CompletedMutation(operationID string, verb Verb, fenceResource string) (Response, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	key := completedMutationKey(operationID, verb, fenceResource)
	if _, released := j.released[key]; released {
		return Response{}, errors.New("broker completed mutation authority is released")
	}
	row, ok := j.completed[key]
	if !ok || validateJournalRow(row) != nil || row.Response == nil || !row.Response.OK {
		return Response{}, errors.New("broker completed mutation authority is unavailable")
	}
	response := *row.Response
	response.Payload = append(json.RawMessage(nil), row.Payload...)
	return response, nil
}

func encodeJournalRow(row journalRow) ([]byte, error) {
	if err := validateJournalRow(row); err != nil {
		return nil, err
	}
	data, err := json.Marshal(row)
	if err != nil || len(data) > maxJournalRowBytes {
		return nil, errors.New("broker journal row exceeds its bounded contract")
	}
	return append(data, '\n'), nil
}

func (j *FileJournal) append(row journalRow, reserveBytes int, reserveRows int) error {
	line, err := encodeJournalRow(row)
	if err != nil {
		return err
	}
	candidate := append(append([]journalRow(nil), j.rows...), row)
	prospectiveBytes := j.fileBytes + int64(len(line))
	if prospectiveBytes > maxJournalBytes || len(candidate) > maxJournalRows ||
		prospectiveBytes+int64(reserveBytes) > maxJournalFileBytes || len(candidate)+reserveRows > maxJournalRows {
		return j.compact(candidate, len(candidate)-1, reserveBytes, reserveRows)
	}
	file, err := j.fs.openFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(line)
	if writeErr == nil && written != len(line) {
		writeErr = io.ErrShortWrite
	}
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	directoryErr := error(nil)
	if writeErr == nil && syncErr == nil && closeErr == nil {
		directoryErr = j.fs.syncDirectory(j.root)
	}
	if writeErr != nil || syncErr != nil || closeErr != nil || directoryErr != nil {
		operationErr := errors.Join(writeErr, syncErr, closeErr, directoryErr)
		return errors.Join(operationErr, j.reloadAfterWriteFailure())
	}
	indexes, err := buildJournalIndexes(candidate)
	if err != nil {
		return err
	}
	j.installRows(candidate, indexes, prospectiveBytes)
	return nil
}

func (j *FileJournal) reloadAfterWriteFailure() error {
	loaded, err := j.readJournalFile(j.path, false, true)
	if errors.Is(err, os.ErrNotExist) {
		j.installRows(nil, newJournalIndexes(), 0)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reload broker journal after write failure: %w", err)
	}
	if loaded.tailOffset >= 0 {
		if err := j.recoverInterruptedTail(loaded.tailOffset); err != nil {
			return err
		}
		loaded.fileBytes = loaded.tailOffset
	}
	j.installRows(loaded.rows, loaded.indexes, loaded.fileBytes)
	return nil
}

func (j *FileJournal) compact(candidate []journalRow, requiredIndex, reserveBytes, reserveRows int) error {
	keep, indexes, encoded, err := retainedJournalProjection(candidate, requiredIndex, reserveBytes, reserveRows)
	if err != nil {
		return err
	}
	temporary := j.path + ".new"
	if exists, statErr := journalPathExists(j.fs, temporary); statErr != nil {
		return statErr
	} else if exists {
		if err := j.removeCompactionTemporary(temporary); err != nil {
			return err
		}
	}
	file, err := j.fs.openFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	published := false
	cleanup := func() error {
		if published {
			return nil
		}
		removeErr := j.fs.remove(temporary)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return removeErr
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(writeErr, syncErr, closeErr, cleanup())
	}
	if err := j.fs.rename(temporary, j.path); err != nil {
		return errors.Join(err, cleanup())
	}
	published = true
	if err := j.fs.syncDirectory(j.root); err != nil {
		return errors.Join(err, j.reloadAfterWriteFailure())
	}
	j.installRows(keep, indexes, int64(len(encoded)))
	return nil
}

func retainedJournalProjection(rows []journalRow, requiredIndex, reserveBytes, reserveRows int) ([]journalRow, journalIndexes, []byte, error) {
	indexes, err := buildJournalIndexes(rows)
	if err != nil {
		return nil, journalIndexes{}, nil, err
	}
	state := journalRow{Schema: 1, Phase: "authority", Authority: &journalAuthorityState{
		Sequence: indexes.sequence, PreviousDigest: indexes.previous, Fences: cloneFenceAuthority(indexes.fences),
	}}
	stateLine, err := encodeJournalRow(state)
	if err != nil {
		return nil, journalIndexes{}, nil, err
	}
	required := make(map[int]struct{})
	if requiredIndex >= 0 {
		required[requiredIndex] = struct{}{}
	}
	for idempotency := range indexes.unresolved {
		for index := len(rows) - 1; index >= 0; index-- {
			if rows[index].Phase == "active" && rows[index].IdempotencyKey == idempotency {
				required[index] = struct{}{}
				break
			}
		}
	}
	for key := range indexes.liveCompleted {
		for index := len(rows) - 1; index >= 0; index-- {
			row := rows[index]
			if row.Phase == "complete" && row.Response != nil && row.Response.OK && row.Receipt != nil &&
				completedMutationKey(row.Receipt.OperationID, row.Receipt.Verb, row.Receipt.FenceResource) == key {
				required[index] = struct{}{}
				break
			}
		}
	}
	selected := make(map[int]struct{}, len(required))
	encodedBytes := len(stateLine)
	for index := range required {
		if rows[index].Phase == "authority" {
			continue
		}
		line, encodeErr := encodeJournalRow(rows[index])
		if encodeErr != nil {
			return nil, journalIndexes{}, nil, encodeErr
		}
		selected[index] = struct{}{}
		encodedBytes += len(line)
	}
	if encodedBytes+reserveBytes > maxJournalFileBytes || 1+len(selected)+reserveRows > maxJournalRows {
		return nil, journalIndexes{}, nil, errors.New("broker live journal authority exceeds its bounded contract")
	}
	for index := len(rows) - 1; index >= 0; index-- {
		if _, exists := selected[index]; exists || rows[index].Phase != "complete" {
			continue
		}
		row := rows[index]
		if row.Response != nil && row.Response.OK && row.Receipt != nil {
			key := completedMutationKey(row.Receipt.OperationID, row.Receipt.Verb, row.Receipt.FenceResource)
			if _, released := indexes.released[key]; released {
				continue
			}
		}
		line, encodeErr := encodeJournalRow(row)
		if encodeErr != nil {
			return nil, journalIndexes{}, nil, encodeErr
		}
		if 1+len(selected)+1+reserveRows > targetJournalRows || encodedBytes+len(line)+reserveBytes > maxJournalBytes {
			continue
		}
		selected[index] = struct{}{}
		encodedBytes += len(line)
	}
	indices := make([]int, 0, len(selected))
	for index := range selected {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	keep := make([]journalRow, 0, len(indices)+1)
	keep = append(keep, state)
	var output bytes.Buffer
	output.Grow(encodedBytes)
	_, _ = output.Write(stateLine)
	for _, index := range indices {
		keep = append(keep, rows[index])
		line, _ := encodeJournalRow(rows[index])
		_, _ = output.Write(line)
	}
	if output.Len()+reserveBytes > maxJournalFileBytes || len(keep)+reserveRows > maxJournalRows {
		return nil, journalIndexes{}, nil, errors.New("broker compacted journal exceeds its bounded contract")
	}
	keptIndexes, err := buildJournalIndexes(keep)
	if err != nil {
		return nil, journalIndexes{}, nil, err
	}
	return keep, keptIndexes, output.Bytes(), nil
}

func cloneFenceAuthority(source map[string]uint64) map[string]uint64 {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]uint64, len(source))
	for resource, sequence := range source {
		result[resource] = sequence
	}
	return result
}

func receiptDigest(receipt Receipt) string {
	receipt.ReceiptDigest = ""
	data, _ := json.Marshal(receipt)
	return Digest(data)
}

func canonicalRequestAuthority(request Request) []byte {
	copy := request
	copy.Payload = nil
	data, _ := json.Marshal(copy)
	return data
}
