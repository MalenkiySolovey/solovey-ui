//go:build linux && !minimal

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

// CommitZapretPath verifies the staged generation and retains its checkpoint
// in the existing file journal. peerRevision comes from the broker's attested
// Panel identity, never from an Outbound or framed request.
func (j *FileJournal) CommitZapretPath(ctx context.Context, path *nftables.ZapretNativePath, observe func(context.Context) (planner.Facts, error), peerRevision string) error {
	if j == nil || path == nil {
		return errZapretSocket
	}
	return path.Commit(ctx, observe, func(checkpoint nftables.ZapretPathCheckpoint) error {
		return j.recordZapretPath(ctx, checkpoint, peerRevision)
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
	if prior, retained := j.liveCompleted[completedMutationKey(zapretPathResource, zapretPathCommit, zapretPathResource)]; retained {
		// This broker's boot ID is read from procfs at native startup. A
		// different kernel boot destroys the old sockets, tables and BPF
		// links. A broker restart or Panel revision change does not.
		boot, err := currentBootID()
		var old nftables.ZapretPathCheckpoint
		if err != nil || boot != j.bootID || !zapretKernelBootID.MatchString(boot) || prior.Receipt == nil ||
			!zapretKernelBootID.MatchString(prior.Receipt.BrokerBootID) || prior.Receipt.BrokerBootID == boot ||
			decodeStrict(prior.Payload, &old) != nil || old.Generation != prior.Receipt.RequestID || old.Generation == checkpoint.Generation {
			return Failure(CodeRecoveryRequired, "previous Zapret egress generation requires guarded recovery")
		}
	}
	return j.writeZapretPathRecordLocked(ctx, checkpoint, zapretPathCommit, "zapret-commit-", checkpoint.Generation, peerRevision,
		CompletionPolicy{RetainUntilRelease: true})
}

// RetireZapretBeforeAdmission releases a retained checkpoint only when its
// live owner can prove it never enrolled a socket and cleanly retired the
// kernel resources. Any admitted or ambiguous generation still needs drain.
func (j *FileJournal) RetireZapretBeforeAdmission(ctx context.Context, path *nftables.ZapretNativePath, peerRevision string) error {
	if j == nil || path == nil || !digestPattern.MatchString(peerRevision) {
		return errZapretSocket
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := completedMutationKey(zapretPathResource, zapretPathCommit, zapretPathResource)
	j.mu.Lock()
	retained, exists := j.liveCompleted[key]
	j.mu.Unlock()
	generation := path.Generation()
	var checkpoint nftables.ZapretPathCheckpoint
	if !exists || retained.Receipt == nil || retained.Receipt.BrokerBootID != j.bootID ||
		retained.Receipt.PeerRevision != peerRevision || decodeStrict(retained.Payload, &checkpoint) != nil || checkpoint.Generation != generation {
		return Failure(CodeRecoveryRequired, "live Zapret generation does not match the retained receipt")
	}
	if err := path.AbortUnadmitted(generation); err != nil {
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

// Stage and retirement run before the journal append and are already fail
// closed. One complete row avoids leaving an active mutation that would
// block unrelated broker actions after an interrupted checkpoint write.
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
	response, err := j.CompletedMutation(zapretPathResource, zapretPathCommit, zapretPathResource)
	if err != nil || response.Receipt == nil || response.Receipt.BrokerBootID != j.bootID || response.Receipt.PeerRevision != grant.PeerRevision {
		return errors.Join(errZapretSocket, err)
	}
	var checkpoint nftables.ZapretPathCheckpoint
	if err := decodeStrict(response.Payload, &checkpoint); err != nil || checkpoint.Generation != grant.Generation ||
		checkpoint.PlanRevision != grant.Scope.PlanRevision() || checkpoint.SocketMark != grant.Scope.Mark() || checkpoint.Egress != grant.Scope.Egress() {
		return errZapretSocket
	}
	return nil
}
