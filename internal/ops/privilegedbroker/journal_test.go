package privilegedbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJournalAppendAndCompactionStayWithinReopenBounds(t *testing.T) {
	root := testJournalRoot(t)
	journal, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`"` + strings.Repeat("x", MaxResponseBytes-4096) + `"`)
	for index := 1; index <= 80; index++ {
		row := testCompletedJournalRow(uint64(index), fmt.Sprintf("operation-%03d", index), fmt.Sprintf("idempotency-%03d", index),
			Verb("deployment.test.apply"), "deployment", uint64(index), payload, false, "")
		if err := journal.append(row, 0, 0); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
		info, err := os.Stat(journal.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxJournalFileBytes || len(journal.rows) > maxJournalRows {
			t.Fatalf("writer exceeded reopen bounds after %d: bytes=%d rows=%d", index, info.Size(), len(journal.rows))
		}
	}
	if journal.fileBytes > maxJournalBytes {
		t.Fatalf("compacted journal did not return to its byte target: %d", journal.fileBytes)
	}
	restarted, err := openTestJournal(root, "boot-two")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.fileBytes != journal.fileBytes || len(restarted.rows) != len(journal.rows) || restarted.fences["deployment"] != 80 {
		t.Fatalf("restart bounds/indexes differ: live bytes=%d rows=%d fence=%d restart bytes=%d rows=%d fence=%d",
			journal.fileBytes, len(journal.rows), journal.fences["deployment"], restarted.fileBytes, len(restarted.rows), restarted.fences["deployment"])
	}
}

func TestCompactionPersistsDormantFenceOutsidePrunableHistory(t *testing.T) {
	rows := []journalRow{testCompletedJournalRow(1, "operation-a", "idempotency-a", Verb("deployment.test.apply"), "resource-a", 900,
		json.RawMessage(`{"value":"a"}`), false, "")}
	for index := 2; index <= targetJournalRows+200; index++ {
		rows = append(rows, testCompletedJournalRow(uint64(index), fmt.Sprintf("operation-b-%04d", index), fmt.Sprintf("idempotency-b-%04d", index),
			Verb("deployment.test.apply"), "resource-b", uint64(index), json.RawMessage(`{"value":"b"}`), false, ""))
	}
	keep, _, encoded, err := retainedJournalProjection(rows, -1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(keep) > targetJournalRows || keep[0].Phase != "authority" || keep[0].Authority.Fences["resource-a"] != 900 {
		t.Fatalf("fence snapshot/retention is invalid: rows=%d state=%#v", len(keep), keep[0])
	}
	for _, row := range keep[1:] {
		if row.FenceResource == "resource-a" {
			t.Fatal("dormant resource history unexpectedly remained in the retained window")
		}
	}
	root := testJournalRoot(t)
	if err := os.WriteFile(filepath.Join(root, journalFileName), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := openTestJournal(root, "boot")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	stale := brokerMutationRequest(t, now, 900, "idempotency-a-stale")
	stale.OperationID = "operation-a-stale"
	stale.Fence.Resource = "resource-a"
	staleDigest := Digest(append(canonicalRequestAuthority(stale), stale.Payload...))
	if _, _, err := restarted.Begin(stale, PeerIdentity{Revision: Digest([]byte("peer"))}, staleDigest, now); publicCode(err) != CodeFence {
		t.Fatalf("dormant fence high-water was not enforced: %v", err)
	}
	fresh := stale
	fresh.Fence.Sequence = 901
	fresh.IdempotencyKey = "idempotency-a-fresh"
	fresh.OperationID = "operation-a-fresh"
	freshDigest := Digest(append(canonicalRequestAuthority(fresh), fresh.Payload...))
	if _, receipt, err := restarted.Begin(fresh, PeerIdentity{Revision: Digest([]byte("peer"))}, freshDigest, now); err != nil || receipt == nil {
		t.Fatalf("strictly newer dormant-resource fence was rejected: receipt=%#v err=%v", receipt, err)
	}
}

func TestCompletedMutationAuthoritySurvivesCompactionUntilAtomicRelease(t *testing.T) {
	stage := testCompletedJournalRow(1, "ssh-operation", "ssh-stage", VerbSSHStage, "ssh-managed-dropin", 1,
		json.RawMessage(`{"checkpoint":"root-owned"}`), true, "")
	rows := []journalRow{stage}
	for index := 2; index <= targetJournalRows+200; index++ {
		rows = append(rows, testCompletedJournalRow(uint64(index), fmt.Sprintf("operation-%04d", index), fmt.Sprintf("idempotency-%04d", index),
			Verb("deployment.test.apply"), "deployment", uint64(index), json.RawMessage(`{"value":"history"}`), false, ""))
	}
	_, _, encoded, err := retainedJournalProjection(rows, -1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	root := testJournalRoot(t)
	if err := os.WriteFile(filepath.Join(root, journalFileName), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompletedMutation("ssh-operation", VerbSSHStage, "ssh-managed-dropin"); err != nil {
		t.Fatalf("live completed mutation was pruned: %v", err)
	}
	release := testCompletedJournalRow(uint64(targetJournalRows+201), "ssh-operation", "ssh-restore", VerbSSHRestore, "ssh-managed-dropin", 5,
		json.RawMessage(`{"restored":true}`), false, VerbSSHStage)
	if err := journal.append(release, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompletedMutation("ssh-operation", VerbSSHStage, "ssh-managed-dropin"); err == nil {
		t.Fatal("released completed mutation remained queryable")
	}
	if err := journal.compact(journal.rows, -1, 0, 0); err != nil {
		t.Fatal(err)
	}
	restarted, err := openTestJournal(root, "boot-two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.CompletedMutation("ssh-operation", VerbSSHStage, "ssh-managed-dropin"); err == nil {
		t.Fatal("released completed mutation returned after compaction/restart")
	}
	for _, row := range restarted.rows {
		if row.Phase == "complete" && row.Receipt != nil && row.Receipt.OperationID == "ssh-operation" && row.Receipt.Verb == VerbSSHStage {
			t.Fatal("released completed mutation was not prunable")
		}
	}
}

func TestInterruptedFinalRowsRecoverEveryCompletePrefixCut(t *testing.T) {
	prior := testCompletedJournalRow(1, "prior-operation", "prior-idempotency", Verb("deployment.test.apply"), "deployment", 1,
		json.RawMessage(`{"value":"prior"}`), false, "")
	priorLine := mustEncodeJournalRow(t, prior)
	active, complete := testJournalPair(2, "cut-operation", "cut-idempotency", Verb("deployment.test.apply"), "deployment", 2,
		json.RawMessage(`{"value":"cut"}`), false, "")
	activeLine := mustEncodeJournalRow(t, active)
	completeLine := mustEncodeJournalRow(t, complete)

	for name, fixture := range map[string]struct {
		prefix     []byte
		final      []byte
		unresolved int
	}{
		"active":   {prefix: priorLine, final: activeLine, unresolved: 0},
		"complete": {prefix: append(append([]byte(nil), priorLine...), activeLine...), final: completeLine, unresolved: 1},
	} {
		t.Run(name, func(t *testing.T) {
			for cut := 0; cut < len(fixture.final); cut++ {
				data := append(append([]byte(nil), fixture.prefix...), fixture.final[:cut]...)
				loaded, err := parseJournal(data, true)
				if err != nil {
					t.Fatalf("cut %d/%d rejected: %v", cut, len(fixture.final), err)
				}
				if len(loaded.indexes.unresolved) != fixture.unresolved {
					t.Fatalf("cut %d unresolved=%d want=%d", cut, len(loaded.indexes.unresolved), fixture.unresolved)
				}
			}
			for _, cut := range []int{1, len(fixture.final) / 2, len(fixture.final) - 1} {
				root := testJournalRoot(t)
				path := filepath.Join(root, journalFileName)
				if err := os.WriteFile(path, append(append([]byte(nil), fixture.prefix...), fixture.final[:cut]...), 0o600); err != nil {
					t.Fatal(err)
				}
				journal, err := openTestJournal(root, "boot")
				if err != nil {
					t.Fatalf("durable cut %d rejected: %v", cut, err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Size() != int64(len(fixture.prefix)) || len(journal.unresolved) != fixture.unresolved {
					t.Fatalf("durable cut %d was not truncated to its complete prefix: bytes=%d unresolved=%d", cut, info.Size(), len(journal.unresolved))
				}
			}
		})
	}
	if _, err := parseJournal(append(append([]byte(nil), priorLine...), []byte("{\"schema\":1}\n")...), true); err == nil {
		t.Fatal("malformed newline-terminated row was accepted")
	}
	if _, err := parseJournal(append(append(append([]byte(nil), priorLine...), '\n'), activeLine...), true); err == nil {
		t.Fatal("empty committed non-tail row was accepted")
	}
}

func TestStaleCompactionTemporaryIsRemovedBeforeThresholdAppend(t *testing.T) {
	rows := make([]journalRow, 0, maxJournalRows)
	for index := 1; index <= maxJournalRows; index++ {
		rows = append(rows, testCompletedJournalRow(uint64(index), fmt.Sprintf("operation-%04d", index), fmt.Sprintf("idempotency-%04d", index),
			Verb("deployment.test.apply"), "deployment", uint64(index), json.RawMessage(`{"value":"history"}`), false, ""))
	}
	root := testJournalRoot(t)
	writeJournalRows(t, filepath.Join(root, journalFileName), rows)
	if err := os.WriteFile(filepath.Join(root, journalFileName)+".new", []byte("interrupted compaction"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal.path + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale compaction temporary remains: %v", err)
	}
	row := testCompletedJournalRow(maxJournalRows+1, "operation-final", "idempotency-final", Verb("deployment.test.apply"), "deployment", maxJournalRows+1,
		json.RawMessage(`{"value":"final"}`), false, "")
	if err := journal.append(row, 0, 0); err != nil {
		t.Fatal(err)
	}
	restarted, err := openTestJournal(root, "boot-two")
	if err != nil {
		t.Fatal(err)
	}
	assertJournalDiskLiveEqual(t, journal, restarted)
	if restarted.fences["deployment"] != maxJournalRows+1 || len(restarted.rows) > maxJournalRows || restarted.fileBytes > maxJournalFileBytes {
		t.Fatalf("threshold append was not recoverably compacted: fence=%d rows=%d bytes=%d", restarted.fences["deployment"], len(restarted.rows), restarted.fileBytes)
	}
}

func TestCompletedCompactionTemporaryIsPromotedWhenMainNameIsAbsent(t *testing.T) {
	root := testJournalRoot(t)
	rows := []journalRow{testCompletedJournalRow(7, "operation-seven", "idempotency-seven", Verb("deployment.test.apply"),
		"deployment", 77, json.RawMessage(`{"value":"published"}`), false, "")}
	_, _, encoded, err := retainedJournalProjection(rows, -1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(root, journalFileName) + ".new"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := openTestJournal(root, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if journal.fences["deployment"] != 77 || journal.sequence != 7 {
		t.Fatalf("recovered compaction authority differs: fence=%d sequence=%d", journal.fences["deployment"], journal.sequence)
	}
	if _, err := os.Stat(journal.path); err != nil {
		t.Fatalf("recovered compaction was not published at the main name: %v", err)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("promoted compaction temporary remains: %v", err)
	}
	restarted, err := openTestJournal(root, "restart")
	if err != nil {
		t.Fatal(err)
	}
	assertJournalDiskLiveEqual(t, journal, restarted)
}

func TestJournalFilesystemFaultsKeepDiskAndLiveIndexesAligned(t *testing.T) {
	t.Run("root create", func(t *testing.T) {
		for _, fault := range []string{"mkdir", "parent-sync"} {
			t.Run(fault, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "solovey-ui-broker")
				fs := realJournalFilesystem()
				if fault == "mkdir" {
					fs.mkdir = func(string, os.FileMode) error { return errors.New("injected root create failure") }
				} else {
					fs.syncDirectory = func(string) error { return errors.New("injected parent sync failure") }
				}
				if _, err := openFileJournal(root, "boot", fs, false); err == nil {
					t.Fatalf("%s fault was acknowledged", fault)
				}
			})
		}
	})

	t.Run("append create and sync", func(t *testing.T) {
		for _, fault := range []string{"file-create", "file-sync", "directory-sync"} {
			t.Run(fault, func(t *testing.T) {
				root := testJournalRoot(t)
				journal := newTestFileJournal(root, "boot")
				fs := realJournalFilesystem()
				if fault == "file-create" {
					original := fs.openFile
					fs.openFile = func(path string, flag int, mode os.FileMode) (journalFile, error) {
						if path == journal.path && flag&os.O_CREATE != 0 {
							return nil, errors.New("injected journal create failure")
						}
						return original(path, flag, mode)
					}
				}
				if fault == "file-sync" {
					fs = journalFilesystemWithFileFault(fs, journal.path, "sync")
				}
				if fault == "directory-sync" {
					failed := false
					original := fs.syncDirectory
					fs.syncDirectory = func(path string) error {
						if path == root && !failed {
							failed = true
							return errors.New("injected journal directory sync failure")
						}
						return original(path)
					}
				}
				journal.fs = fs
				now := time.Unix(1_900_000_000, 0).UTC()
				request := brokerMutationRequest(t, now, 1, "fault-idempotency")
				digest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
				if _, _, err := journal.Begin(request, PeerIdentity{Revision: Digest([]byte("peer"))}, digest, now); err == nil {
					t.Fatalf("%s fault was acknowledged", fault)
				}
				restarted, err := openTestJournal(root, "restart")
				if err != nil {
					t.Fatal(err)
				}
				assertJournalDiskLiveEqual(t, journal, restarted)
			})
		}
	})

	for _, fault := range []string{"temporary-create", "temporary-write", "temporary-sync", "temporary-close", "rename", "directory-sync"} {
		t.Run("compaction "+fault, func(t *testing.T) {
			root := testJournalRoot(t)
			seed := []journalRow{testCompletedJournalRow(1, "operation-one", "idempotency-one", Verb("deployment.test.apply"), "deployment", 1,
				json.RawMessage(`{"value":"one"}`), false, "")}
			writeJournalRows(t, filepath.Join(root, journalFileName), seed)
			journal, err := openTestJournal(root, "boot")
			if err != nil {
				t.Fatal(err)
			}
			candidate := append(append([]journalRow(nil), journal.rows...), testCompletedJournalRow(2, "operation-two", "idempotency-two",
				Verb("deployment.test.apply"), "deployment", 2, json.RawMessage(`{"value":"two"}`), false, ""))
			fs := realJournalFilesystem()
			temporary := journal.path + ".new"
			switch fault {
			case "temporary-create":
				original := fs.openFile
				fs.openFile = func(path string, flag int, mode os.FileMode) (journalFile, error) {
					if path == temporary {
						return nil, errors.New("injected compaction create failure")
					}
					return original(path, flag, mode)
				}
			case "temporary-write", "temporary-sync", "temporary-close":
				fs = journalFilesystemWithFileFault(fs, temporary, strings.TrimPrefix(fault, "temporary-"))
			case "rename":
				fs.rename = func(string, string) error { return errors.New("injected compaction rename failure") }
			case "directory-sync":
				failed := false
				original := fs.syncDirectory
				fs.syncDirectory = func(path string) error {
					if path == root && !failed {
						failed = true
						return errors.New("injected compaction directory sync failure")
					}
					return original(path)
				}
			}
			journal.fs = fs
			if err := journal.compact(candidate, len(candidate)-1, 0, 0); err == nil {
				t.Fatalf("%s fault was acknowledged", fault)
			}
			restarted, err := openTestJournal(root, "restart")
			if err != nil {
				t.Fatal(err)
			}
			assertJournalDiskLiveEqual(t, journal, restarted)
		})
	}
}

type faultJournalFile struct {
	journalFile
	fault string
}

func (f *faultJournalFile) Write(data []byte) (int, error) {
	if f.fault == "write" {
		return 0, errors.New("injected file write failure")
	}
	return f.journalFile.Write(data)
}

func (f *faultJournalFile) Sync() error {
	if f.fault == "sync" {
		return errors.New("injected file sync failure")
	}
	return f.journalFile.Sync()
}

func (f *faultJournalFile) Close() error {
	err := f.journalFile.Close()
	if f.fault == "close" {
		return errors.Join(err, errors.New("injected file close failure"))
	}
	return err
}

func journalFilesystemWithFileFault(fs journalFilesystem, target, fault string) journalFilesystem {
	original := fs.openFile
	fs.openFile = func(path string, flag int, mode os.FileMode) (journalFile, error) {
		file, err := original(path, flag, mode)
		if err != nil || path != target {
			return file, err
		}
		return &faultJournalFile{journalFile: file, fault: fault}, nil
	}
	return fs
}

func testJournalRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "solovey-ui-broker")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func newTestFileJournal(root, bootID string) *FileJournal {
	journal := &FileJournal{root: root, path: filepath.Join(root, journalFileName), bootID: bootID, fs: realJournalFilesystem()}
	journal.installRows(nil, newJournalIndexes(), 0)
	return journal
}

func testCompletedJournalRow(sequence uint64, operationID, idempotency string, verb Verb, resource string, fence uint64,
	payload json.RawMessage, retain bool, releases Verb,
) journalRow {
	_, complete := testJournalPair(sequence, operationID, idempotency, verb, resource, fence, payload, retain, releases)
	return complete
}

func testJournalPair(sequence uint64, operationID, idempotency string, verb Verb, resource string, fence uint64,
	payload json.RawMessage, retain bool, releases Verb,
) (journalRow, journalRow) {
	requestDigest := Digest([]byte("request-" + idempotency))
	activeReceipt := Receipt{Sequence: sequence, RequestID: "request-" + idempotency, OperationID: operationID, IdempotencyKey: idempotency,
		Verb: verb, FenceResource: resource, FenceSequence: fence, FenceTokenDigest: Digest([]byte("token-" + idempotency)),
		PayloadDigest: Digest([]byte("payload-" + idempotency)), PeerRevision: Digest([]byte("peer")), BrokerBootID: "boot",
		PreviousDigest: Digest([]byte(fmt.Sprintf("previous-%d", sequence))), Outcome: "active", StartedAt: int64(sequence)}
	activeReceipt.ReceiptDigest = receiptDigest(activeReceipt)
	activeCopy := activeReceipt
	active := journalRow{Schema: 1, Phase: "active", IdempotencyKey: idempotency, RequestDigest: requestDigest,
		FenceResource: resource, FenceSequence: fence, StartedAt: activeReceipt.StartedAt, Receipt: &activeCopy}

	completeReceipt := activeReceipt
	completeReceipt.Outcome = "succeeded"
	completeReceipt.CompletedAt = int64(sequence) + 1
	completeReceipt.PreviousDigest = activeReceipt.ReceiptDigest
	responseAuthority := Response{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, RequestID: completeReceipt.RequestID,
		OperationID: operationID, Verb: verb, OK: true, PayloadDigest: Digest(payload)}
	encoded, _ := json.Marshal(responseAuthority)
	completeReceipt.ResponseDigest = Digest(append(encoded, payload...))
	completeReceipt.ReceiptDigest = receiptDigest(completeReceipt)
	completeCopy := completeReceipt
	response := responseAuthority
	response.Receipt = &completeCopy
	retained := retain
	complete := journalRow{Schema: 1, Phase: "complete", IdempotencyKey: idempotency, RequestDigest: requestDigest,
		FenceResource: resource, FenceSequence: fence, StartedAt: activeReceipt.StartedAt, Response: &response, Receipt: &completeCopy,
		Payload: append(json.RawMessage(nil), payload...), RetainUntilRelease: &retained}
	if releases != "" {
		complete.Releases = &completedMutationReference{OperationID: operationID, Verb: releases, FenceResource: resource}
	}
	return active, complete
}

func mustEncodeJournalRow(t *testing.T, row journalRow) []byte {
	t.Helper()
	line, err := encodeJournalRow(row)
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func writeJournalRows(t *testing.T, path string, rows []journalRow) {
	t.Helper()
	var data bytes.Buffer
	for _, row := range rows {
		line := mustEncodeJournalRow(t, row)
		if _, err := data.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertJournalDiskLiveEqual(t *testing.T, live, reopened *FileJournal) {
	t.Helper()
	if !reflect.DeepEqual(live.rows, reopened.rows) || !reflect.DeepEqual(live.fences, reopened.fences) ||
		!reflect.DeepEqual(live.unresolved, reopened.unresolved) || !reflect.DeepEqual(live.completed, reopened.completed) ||
		live.sequence != reopened.sequence || live.previous != reopened.previous || live.fileBytes != reopened.fileBytes {
		t.Fatalf("live journal indexes differ from disk after return:\nlive rows=%d sequence=%d previous=%s bytes=%d fences=%v unresolved=%v\ndisk rows=%d sequence=%d previous=%s bytes=%d fences=%v unresolved=%v",
			len(live.rows), live.sequence, live.previous, live.fileBytes, live.fences, live.unresolved,
			len(reopened.rows), reopened.sequence, reopened.previous, reopened.fileBytes, reopened.fences, reopened.unresolved)
	}
}

var _ io.Writer = (*faultJournalFile)(nil)
