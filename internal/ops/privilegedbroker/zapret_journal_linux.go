//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// This record is local to the privileged owner, not a framed broker verb.
const zapretPathCommit Verb = "zapret.egress.commit"
const zapretPathResource = "zapret-native-egress"

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
	payload, digest, err := MarshalPayload(checkpoint)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: j.bootID,
		Role: RolePanel, Verb: zapretPathCommit, RequestID: checkpoint.Generation, OperationID: zapretPathResource,
		IdempotencyKey: "zapret-commit-" + checkpoint.Generation, Fence: Fence{Resource: zapretPathResource, Sequence: sequence, Token: checkpoint.Generation},
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
	_, err = j.Commit(request, active, response, CompletionPolicy{RetainUntilRelease: true}, time.Now().UTC())
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
