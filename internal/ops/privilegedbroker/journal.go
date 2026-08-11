package privilegedbroker

import (
	"bufio"
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
	journalFileName = "receipts-v1.jsonl"
	maxJournalBytes = 8 << 20
	maxJournalRows  = 4096
)

type journalRow struct {
	Schema         int             `json:"schema"`
	Phase          string          `json:"phase"`
	IdempotencyKey string          `json:"idempotencyKey"`
	RequestDigest  string          `json:"requestDigest"`
	FenceResource  string          `json:"fenceResource"`
	FenceSequence  uint64          `json:"fenceSequence"`
	StartedAt      int64           `json:"startedAt"`
	Response       *Response       `json:"response,omitempty"`
	Receipt        *Receipt        `json:"receipt,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

type replayRecord struct {
	requestDigest string
	response      Response
}

type Journal interface {
	Begin(Request, PeerIdentity, string, time.Time) (*Response, *Receipt, error)
	Commit(Request, *Receipt, Response, time.Time) (Response, error)
	Unresolved() []Receipt
}

type FileJournal struct {
	mu         sync.Mutex
	root       string
	path       string
	bootID     string
	sequence   uint64
	previous   string
	replays    map[string]replayRecord
	fences     map[string]uint64
	unresolved map[string]Receipt
	rows       []journalRow
}

func OpenFileJournal(root, bootID string) (*FileJournal, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || filepath.Base(root) != "solovey-ui-broker" {
		return nil, errors.New("broker journal root is not the fixed dedicated directory")
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	journal := &FileJournal{root: root, path: filepath.Join(root, journalFileName), bootID: bootID,
		replays: make(map[string]replayRecord), fences: make(map[string]uint64), unresolved: make(map[string]Receipt)}
	if err := journal.load(); err != nil {
		return nil, err
	}
	return journal, nil
}

func ensurePrivateDirectory(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(root, 0o700); err != nil {
			return fmt.Errorf("create broker journal root: %w", err)
		}
		info, err = os.Lstat(root)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("broker journal root ownership mode is unsafe")
	}
	return validateOwnedByRoot(root)
}

func (j *FileJournal) load() error {
	file, err := os.Open(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxJournalBytes*2 {
		return errors.New("broker receipt journal is unsafe")
	}
	if err := validateOwnedByRoot(j.path); err != nil {
		return err
	}
	return j.loadRows(file)
}

func (j *FileJournal) loadRows(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), MaxResponseBytes*2)
	for scanner.Scan() {
		var row journalRow
		if err := decodeStrict(scanner.Bytes(), &row); err != nil || row.Schema != 1 || row.IdempotencyKey == "" || !digestPattern.MatchString(row.RequestDigest) {
			return errors.New("broker receipt journal contains an invalid record")
		}
		j.rows = append(j.rows, row)
		if row.Receipt != nil && row.Receipt.Sequence > j.sequence {
			j.sequence = row.Receipt.Sequence
			j.previous = row.Receipt.ReceiptDigest
		}
		if row.FenceSequence > j.fences[row.FenceResource] {
			j.fences[row.FenceResource] = row.FenceSequence
		}
		switch row.Phase {
		case "active":
			if row.Receipt == nil {
				return errors.New("broker active receipt is absent")
			}
			j.unresolved[row.IdempotencyKey] = *row.Receipt
		case "complete":
			if row.Response == nil || row.Receipt == nil {
				return errors.New("broker completed receipt is incomplete")
			}
			response := *row.Response
			response.Payload = append(json.RawMessage(nil), row.Payload...)
			j.replays[row.IdempotencyKey] = replayRecord{requestDigest: row.RequestDigest, response: response}
			delete(j.unresolved, row.IdempotencyKey)
		default:
			return errors.New("broker receipt journal phase is invalid")
		}
	}
	return scanner.Err()
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
	row := journalRow{Schema: 1, Phase: "active", IdempotencyKey: request.IdempotencyKey,
		RequestDigest: requestDigest, FenceResource: request.Fence.Resource, FenceSequence: request.Fence.Sequence,
		StartedAt: receipt.StartedAt, Receipt: receipt}
	if err := j.append(row); err != nil {
		return nil, nil, err
	}
	j.fences[request.Fence.Resource] = request.Fence.Sequence
	j.unresolved[request.IdempotencyKey] = *receipt
	j.previous = receipt.ReceiptDigest
	return nil, receipt, nil
}

func (j *FileJournal) Commit(request Request, receipt *Receipt, response Response, now time.Time) (Response, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if receipt == nil {
		return response, errors.New("broker active receipt is required")
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
	if err := j.append(row); err != nil {
		return response, err
	}
	j.replays[request.IdempotencyKey] = replayRecord{requestDigest: requestDigest, response: response}
	delete(j.unresolved, request.IdempotencyKey)
	j.previous = receipt.ReceiptDigest
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

func (j *FileJournal) append(row journalRow) error {
	data, err := json.Marshal(row)
	if err != nil || len(data) > MaxResponseBytes*2 {
		return errors.New("broker journal row exceeds its bounded contract")
	}
	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	j.rows = append(j.rows, row)
	if len(j.rows) > maxJournalRows {
		return j.compact()
	}
	return nil
}

func (j *FileJournal) compact() error {
	keep := retainedJournalRows(j.rows)
	temporary := j.path + ".new"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(temporary)
		}
	}()
	for _, row := range keep {
		data, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, j.path); err != nil {
		return err
	}
	ok = true
	j.rows = append([]journalRow(nil), keep...)
	j.rebuildIndexes()
	return nil
}

func retainedJournalRows(rows []journalRow) []journalRow {
	limit := maxJournalRows / 2
	if len(rows) <= limit {
		return append([]journalRow(nil), rows...)
	}
	return append([]journalRow(nil), rows[len(rows)-limit:]...)
}

func (j *FileJournal) rebuildIndexes() {
	j.replays = make(map[string]replayRecord)
	j.fences = make(map[string]uint64)
	j.unresolved = make(map[string]Receipt)
	for _, row := range j.rows {
		if row.FenceSequence > j.fences[row.FenceResource] {
			j.fences[row.FenceResource] = row.FenceSequence
		}
		switch row.Phase {
		case "active":
			if row.Receipt != nil {
				j.unresolved[row.IdempotencyKey] = *row.Receipt
			}
		case "complete":
			if row.Response != nil {
				response := *row.Response
				response.Payload = append(json.RawMessage(nil), row.Payload...)
				j.replays[row.IdempotencyKey] = replayRecord{requestDigest: row.RequestDigest, response: response}
			}
			delete(j.unresolved, row.IdempotencyKey)
		}
	}
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
