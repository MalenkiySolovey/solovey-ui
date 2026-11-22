//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

const zapretSystemdResource = "zapret-systemd-mark"

// reserveZapretMark consumes a serial before any socket can receive it. A
// failed or interrupted activation still cannot reuse the old serial. Use a
// completed journal row: there is nothing to replay after a broker restart.
func (j *FileJournal) reserveZapretMark(ctx context.Context, peer PeerIdentity, target zapretactivation.Request, generation string) (uint32, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	sequence := j.fences[zapretSystemdResource] + 1
	if sequence == 0 || sequence > uint64(planner.ZapretMaxSerial) || j.sequence+1 == 0 || len(j.unresolved) != 0 ||
		peer.BootID != j.bootID || !digestPattern.MatchString(peer.Revision) || !digestPattern.MatchString(generation) {
		return 0, Failure(CodeRecoveryRequired, "Zapret mark reservation requires journal recovery")
	}
	payload, digest, err := MarshalPayload(target)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: j.bootID,
		Role: RolePanel, Verb: "zapret.path.reserve", RequestID: generation, OperationID: zapretSystemdResource,
		IdempotencyKey: "zapret-reserve-" + generation, Fence: Fence{Resource: zapretSystemdResource, Sequence: sequence, Token: generation},
		DeadlineAt: now.Add(15 * time.Second).UnixMilli(), Payload: payload, PayloadDigest: digest}
	receipt := j.newMutationReceiptLocked(request, peer.Revision, now)
	response := successResponse(request)
	if _, err := j.commitLocked(request, receipt, response, CompletionPolicy{}, now); err != nil {
		return 0, err
	}
	return uint32(sequence), nil
}
