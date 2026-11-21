//go:build linux && zapretproof

package outbounds

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// ZapretProbeInput derives the qualification plan's input identity from the
// same row and attachment decoder as normal Outbound assembly.
func ZapretProbeInput(row model.Outbound) (hostoutbounds.ZapretBinding, string, error) {
	fields, attachment, err := zapretOptions(row)
	if err != nil {
		return hostoutbounds.ZapretBinding{}, "", err
	}
	if attachment == nil || !attachment.Enabled || row.RemoteMissing || fields["routing_mark"] != nil {
		return hostoutbounds.ZapretBinding{}, "", ErrZapretPacketPathUnqualified
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return hostoutbounds.ZapretBinding{}, "", err
	}
	sum := sha256.Sum256(encoded)
	return *attachment, hex.EncodeToString(sum[:]), nil
}

// ProjectZapretProbe is only compiled by the namespace qualification. It
// cannot project an enabled binding with a stale or different Planner plan.
func ProjectZapretProbe(row model.Outbound, plan planner.ZapretPlan) (json.RawMessage, error) {
	if plan.SocketMark() == 0 {
		native, err := projectZapretOutbound(row)
		if err != nil {
			return nil, err
		}
		return json.Marshal(native)
	}
	attachment, revision, err := ZapretProbeInput(row)
	if err != nil {
		return nil, err
	}
	scope := plan.PacketScope()
	if scope.OutboundRef != attachment.OutboundRef || scope.StrategyRef != attachment.StrategyRef || scope.InputRevision != revision {
		return nil, ErrZapretPacketPathUnqualified
	}
	fields, _, err := zapretOptions(row)
	if err != nil {
		return nil, err
	}
	delete(fields, zapretOption)
	fields["routing_mark"] = json.RawMessage(strconv.Quote("0x" + strconv.FormatUint(uint64(plan.SocketMark()), 16)))
	row.Options, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return json.Marshal(row)
}
