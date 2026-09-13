package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestHistoryTerminalHistoryPrunesByCountAgeAndLogicalBytesWithoutLiveClosureLoss(t *testing.T) {
	fixture := newLifecycleFixture(t)
	now := fixture.manager.now()
	current := historyHistoryOperation(180, StateApplied, now.Unix())
	if err := fixture.db.Create(&model.UpdateReleaseState{Channel: string(release.ChannelMain), LastObservedSequence: current.Sequence,
		LastVerifiedSequence: current.Sequence, LastAppliedSequence: current.Sequence, ReleaseID: current.ReleaseID,
		ManifestDigest: current.ManifestDigest, Version: current.Version, SigningKeyID: "release-key", ExpiresAt: now.Add(time.Hour).Unix(), UpdatedAt: now.Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	for index := uint64(1); index <= 180; index++ {
		state := StateFailed
		if index%11 == 0 {
			state = StateRolledBack
		}
		updatedAt := now.Add(-time.Hour).Unix()
		if index <= 40 {
			updatedAt = now.Add(-updateTerminalHistoryAge - time.Hour).Unix()
		}
		operation := historyHistoryOperation(index, state, updatedAt)
		if index == current.Sequence {
			operation = current
		}
		if err := fixture.db.Create(&operation).Error; err != nil {
			t.Fatal(err)
		}
		for journalIndex := 0; journalIndex < 8; journalIndex++ {
			journal := journalFor(operation, "history_event", "history_reason")
			journal.CreatedAt = updatedAt
			if err := fixture.db.Create(journal).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := fixture.manager.repo.pruneHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	var operations []model.UpdateOperation
	if err := fixture.db.Order("sequence ASC").Find(&operations).Error; err != nil {
		t.Fatal(err)
	}
	if len(operations) > updateTerminalHistoryCount+1 {
		t.Fatalf("terminal history count=%d exceeds closure+horizon", len(operations))
	}
	var retainedCurrent model.UpdateOperation
	if err := fixture.db.First(&retainedCurrent, "operation_id = ?", current.OperationID).Error; err != nil || retainedCurrent.State != string(StateApplied) {
		t.Fatalf("current applied closure was pruned: %#v err=%v", retainedCurrent, err)
	}
	var oldCount int64
	if err := fixture.db.Model(&model.UpdateOperation{}).Where("updated_at < ?", now.Add(-updateTerminalHistoryAge).Unix()).Count(&oldCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Fatalf("old terminal history remains=%d", oldCount)
	}
	var journals int64
	if err := fixture.db.Model(&model.UpdateJournal{}).Count(&journals).Error; err != nil {
		t.Fatal(err)
	}
	if journals > updateTerminalJournalCount {
		t.Fatalf("journal history count=%d exceeds horizon", journals)
	}
	page, truncated, err := fixture.manager.Timeline(context.Background(), current.OperationID, 0, 1)
	if err != nil || len(page) != 1 || !truncated || page[0].OperationID != current.OperationID {
		t.Fatalf("retained current timeline page=%#v truncated=%v err=%v", page, truncated, err)
	}
}

func historyHistoryOperation(sequence uint64, state State, updatedAt int64) model.UpdateOperation {
	digest := historyHistoryDigest(sequence)
	return model.UpdateOperation{OperationID: "update-operation:history-history-" + string(rune('a'+sequence%26)) + "-" + hex.EncodeToString([]byte{byte(sequence)}),
		IdempotencyKey: "idempotency:history-history-" + string(rune('a'+sequence%26)) + "-" + hex.EncodeToString([]byte{byte(sequence)}), State: string(state), Channel: string(release.ChannelMain),
		Sequence: sequence, ReleaseID: "solovey-ui-history-" + string(rune('a'+sequence%26)), Version: "2026.4.0", ManifestDigest: digest,
		ArtifactSetDigest: historyHistoryDigest("artifacts" + string(rune(sequence))), Platform: "linux", Arch: "amd64", BinaryProfile: "full",
		DeploymentRevision: historyHistoryDigest("deployment"), BrokerCapability: "broker-capabilities-1.3", MigrationSetDigest: historyHistoryDigest("migration"),
		RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic", BytesTotal: 1, Revision: 2,
		CreatedAt: updatedAt - 1, UpdatedAt: updatedAt}
}

func historyHistoryDigest(value any) string {
	sum := sha256.Sum256([]byte(fmt.Sprint(value)))
	return hex.EncodeToString(sum[:])
}
