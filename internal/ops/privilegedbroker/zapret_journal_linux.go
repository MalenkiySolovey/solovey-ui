//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
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
	_, retained := j.liveCompleted[completedMutationKey(zapretPathResource, zapretPathCommit, zapretPathResource)]
	sequence := j.fences[zapretPathResource] + 1
	j.mu.Unlock()
	if retained || sequence == 0 {
		return Failure(CodeRecoveryRequired, "previous Zapret egress generation requires guarded recovery")
	}
	return j.writeZapretPathRecord(checkpoint, zapretPathCommit, "zapret-commit-", checkpoint.Generation, peerRevision, sequence,
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
	j.mu.Lock()
	retained, exists := j.liveCompleted[completedMutationKey(zapretPathResource, zapretPathCommit, zapretPathResource)]
	sequence := j.fences[zapretPathResource] + 1
	j.mu.Unlock()
	generation := path.Generation()
	var checkpoint nftables.ZapretPathCheckpoint
	if !exists || sequence == 0 || retained.Receipt == nil || retained.Receipt.BrokerBootID != j.bootID ||
		retained.Receipt.PeerRevision != peerRevision || decodeStrict(retained.Payload, &checkpoint) != nil || checkpoint.Generation != generation {
		return Failure(CodeRecoveryRequired, "live Zapret generation does not match the retained receipt")
	}
	if err := path.AbortUnadmitted(generation); err != nil {
		return errors.Join(Failure(CodeRecoveryRequired, "Zapret generation requires guarded recovery"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	retirement := struct{ Generation, CommitReceipt string }{generation, retained.Receipt.ReceiptDigest}
	return j.writeZapretPathRecord(retirement, zapretPathRetire, "zapret-retire-", generation, peerRevision, sequence,
		CompletionPolicy{ReleasesVerb: zapretPathCommit})
}

func (j *FileJournal) writeZapretPathRecord(value any, verb Verb, prefix, generation, peerRevision string, sequence uint64, policy CompletionPolicy) error {
	payload, digest, err := MarshalPayload(value)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: j.bootID,
		Role: RolePanel, Verb: verb, RequestID: generation, OperationID: zapretPathResource,
		IdempotencyKey: prefix + generation, Fence: Fence{Resource: zapretPathResource, Sequence: sequence, Token: generation},
		DeadlineAt: now.Add(30 * time.Second).UnixMilli(), Payload: payload, PayloadDigest: digest}
	requestDigest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
	replay, active, err := j.Begin(request, PeerIdentity{Revision: peerRevision}, requestDigest, now)
	if err != nil {
		return err
	}
	if replay != nil || active == nil {
		return Failure(CodeRecoveryRequired, "cannot reuse a Zapret egress receipt")
	}
	response := successResponse(request)
	response.Payload, response.PayloadDigest = payload, digest
	_, err = j.Commit(request, active, response, policy, time.Now().UTC())
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
