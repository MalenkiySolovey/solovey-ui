//go:build linux && !minimal && zapretproof

package privilegedbroker

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// These records are local to the privileged owner, not framed broker verbs.
const (
	zapretPathCommit   Verb = "zapret.egress.commit"
	zapretPathRetire   Verb = "zapret.egress.retire"
	zapretPathResource      = "zapret-native-egress"
)

var zapretKernelBootID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type zapretPathState string

const (
	zapretPathPrepared zapretPathState = "prepared"
	zapretPathMayLease zapretPathState = "may_have_leased"
)

type zapretPathRecord struct {
	nftables.ZapretPathCheckpoint
	State zapretPathState `json:"state,omitempty"`
}

func zapretPathKey() string {
	return completedMutationKey(zapretPathResource, zapretPathCommit, zapretPathResource)
}

func readZapretPath(row journalRow) (zapretPathRecord, bool) {
	var record zapretPathRecord
	valid := row.Receipt != nil && row.Response != nil && row.Response.OK && validateJournalRow(row) == nil &&
		decodeStrict(row.Payload, &record) == nil && record.Generation == row.Receipt.RequestID &&
		digestPattern.MatchString(record.Generation) && digestPattern.MatchString(record.PlanRevision)
	return record, valid
}

// CommitZapretPath verifies the staged generation and retains its checkpoint
// in the existing file journal. peerRevision comes from the broker's attested
// Panel identity, never from an Outbound or framed request.
func (j *FileJournal) CommitZapretPath(ctx context.Context, path *nftables.ZapretNativePath, observe func(context.Context) (planner.Facts, error), peerRevision string) error {
	if j == nil || path == nil {
		return errZapretSocket
	}
	return path.Commit(ctx, observe, func(ctx context.Context, checkpoint nftables.ZapretPathCheckpoint) error {
		return j.recordZapretPath(ctx, checkpoint, peerRevision)
	}, func(ctx context.Context, checkpoint nftables.ZapretPathCheckpoint) error {
		return j.recordFirstZapretLease(ctx, checkpoint, peerRevision)
	})
}

func (j *FileJournal) recordZapretPath(ctx context.Context, checkpoint nftables.ZapretPathCheckpoint, peerRevision string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !digestPattern.MatchString(peerRevision) || !digestPattern.MatchString(checkpoint.Generation) || !digestPattern.MatchString(checkpoint.PlanRevision) {
		return errZapretSocket
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if prior, retained := j.liveCompleted[zapretPathKey()]; retained {
		// This broker's boot ID is read from procfs at native startup. A
		// different kernel boot destroys the old sockets, tables and BPF
		// links. A broker restart or Panel revision change does not.
		boot, err := currentBootID()
		old, valid := readZapretPath(prior)
		if err != nil || boot != j.bootID || !zapretKernelBootID.MatchString(boot) || !valid ||
			!zapretKernelBootID.MatchString(prior.Receipt.BrokerBootID) || old.Generation == checkpoint.Generation {
			return Failure(CodeRecoveryRequired, "previous Zapret egress generation requires guarded recovery")
		}
		if prior.Receipt.BrokerBootID == boot && (old.State != zapretPathPrepared || !checkpoint.ExcludesPriorRegistration(old.ZapretPathCheckpoint)) {
			return Failure(CodeRecoveryRequired, "previous Zapret egress generation requires guarded recovery")
		}
		// In the same boot, only a newly verified registration at the same
		// exclusive table and hook slots can replace a prepared generation.
	}
	return j.writeZapretPathRecordLocked(ctx, zapretPathRecord{checkpoint, zapretPathPrepared}, zapretPathCommit, "zapret-commit-", checkpoint.Generation, peerRevision,
		CompletionPolicy{RetainUntilRelease: true})
}

// recordFirstZapretLease replaces the retained prepared record atomically.
// It is called under the live path lock, before the guard can pass packets.
func (j *FileJournal) recordFirstZapretLease(ctx context.Context, checkpoint nftables.ZapretPathCheckpoint, peerRevision string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	retained := j.liveCompleted[zapretPathKey()]
	record, valid := readZapretPath(retained)
	if !valid || record.State != zapretPathPrepared || record.ZapretPathCheckpoint != checkpoint ||
		retained.Receipt.BrokerBootID != j.bootID || retained.Receipt.PeerRevision != peerRevision {
		return Failure(CodeRecoveryRequired, "first Zapret lease does not match the prepared generation")
	}
	record.State = zapretPathMayLease
	return j.writeZapretPathRecordLocked(ctx, record, zapretPathCommit, "zapret-lease-", checkpoint.Generation, peerRevision,
		CompletionPolicy{RetainUntilRelease: true})
}

// RetireZapretUnleased releases a retained checkpoint only when its live
// owner can prove it never opened a lease and cleanly retired its resources.
func (j *FileJournal) RetireZapretUnleased(ctx context.Context, path *nftables.ZapretNativePath, peerRevision string) error {
	if j == nil || path == nil || !digestPattern.MatchString(peerRevision) {
		return errZapretSocket
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := zapretPathKey()
	j.mu.Lock()
	retained := j.liveCompleted[key]
	j.mu.Unlock()
	generation := path.Generation()
	record, valid := readZapretPath(retained)
	if !valid || record.State != zapretPathPrepared || retained.Receipt.BrokerBootID != j.bootID ||
		retained.Receipt.PeerRevision != peerRevision || record.Generation != generation {
		return Failure(CodeRecoveryRequired, "live Zapret generation does not match the retained receipt")
	}
	if err := path.AbortUnleased(generation); err != nil {
		return errors.Join(Failure(CodeRecoveryRequired, "Zapret generation requires guarded recovery"), err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if current := j.liveCompleted[key]; current.Receipt == nil || current.Receipt.ReceiptDigest != retained.Receipt.ReceiptDigest {
		return Failure(CodeRecoveryRequired, "retained Zapret egress generation changed during retirement")
	}
	retirement := struct{ Generation, CommitReceipt string }{generation, retained.Receipt.ReceiptDigest}
	return j.writeZapretPathRecordLocked(ctx, retirement, zapretPathRetire, "zapret-retire-", generation, peerRevision,
		CompletionPolicy{ReleasesVerb: zapretPathCommit})
}

// Use one complete row per transition so an interrupted write does not
// leave an active mutation that blocks unrelated broker operations.
func (j *FileJournal) writeZapretPathRecordLocked(ctx context.Context, value any, verb Verb, prefix, generation, peerRevision string, policy CompletionPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sequence := j.fences[zapretPathResource] + 1
	if sequence == 0 || j.sequence+1 == 0 || len(j.unresolved) != 0 {
		return Failure(CodeRecoveryRequired, "broker journal has unresolved or exhausted mutation authority")
	}
	idempotency := prefix + generation
	if _, exists := j.replays[idempotency]; exists {
		return Failure(CodeRecoveryRequired, "cannot reuse a Zapret egress receipt")
	}
	payload, digest, err := MarshalPayload(value)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: j.bootID,
		Role: RolePanel, Verb: verb, RequestID: generation, OperationID: zapretPathResource,
		IdempotencyKey: idempotency, Fence: Fence{Resource: zapretPathResource, Sequence: sequence, Token: generation},
		DeadlineAt: now.Add(30 * time.Second).UnixMilli(), Payload: payload, PayloadDigest: digest}
	receipt := j.newMutationReceiptLocked(request, peerRevision, now)
	response := successResponse(request)
	response.Payload, response.PayloadDigest = payload, digest
	_, err = j.commitLocked(request, receipt, response, policy, now)
	return err
}

func (j *FileJournal) checkZapretPath(grant ZapretSocketGrant) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	retained := j.liveCompleted[zapretPathKey()]
	checkpoint, valid := readZapretPath(retained)
	if !valid || checkpoint.State != zapretPathMayLease || retained.Receipt.BrokerBootID != j.bootID ||
		retained.Receipt.PeerRevision != grant.PeerRevision || checkpoint.Generation != grant.Generation ||
		checkpoint.PlanRevision != grant.Scope.PlanRevision() || checkpoint.SocketMark != grant.Scope.Mark() || checkpoint.Egress != grant.Scope.Egress() {
		return errZapretSocket
	}
	return nil
}
