//go:build linux && !minimal

package nftables

import (
	"encoding/binary"
	"slices"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	zapretReplyQueue    = "local_reply"
	zapretReplyVerify   = "verify_reply"
	zapretReplyPrecheck = "deny_premarked_reply"
	zapretReplyDeny     = "deny_unadmitted_reply"
	// Earlier INPUT policy may retain ordinary drop semantics. No foreign
	// INPUT hook may interleave with or follow the final approval block.
	ZapretReplyGuardPriority    = zapretGuardPriority
	ZapretReplyPrecheckPriority = ZapretReplyGuardPriority - 3
	zapretReplyQueuePriority    = ZapretReplyPrecheckPriority + 1
)

func zapretHasReplyPath(plans []planner.ZapretPlan) bool {
	return slices.ContainsFunc(plans, planner.ZapretPlan.NeedsReturn)
}

func (b *zapretBatch) addZapretReplyPath(plan planner.ZapretPlan) error {
	scope, err := plan.SocketScope()
	if err != nil {
		return err
	}
	egress := scope.Egress()
	for _, ipv6 := range []bool{false, true} {
		if !scope.AllowsFamily(intent.PacketProtocolTCP, ipv6) {
			continue
		}
		family, addressOffset := byte(unix.NFPROTO_IPV4), uint32(16)
		if ipv6 {
			family, addressOffset = unix.NFPROTO_IPV6, 24
		}
		observe := plan.ReplyTCPForFamily(ipv6)
		for _, port := range plan.PortsForFamily(intent.PacketProtocolTCP, ipv6) {
			rule := tunMetaEq(expr.MetaKeyNFPROTO, []byte{family})
			rule = append(rule, zapretTCPSocketMark(plan.SocketMark(), ^uint32(0))...)
			rule = append(rule, &expr.Ct{Key: expr.CtKeyDIRECTION, Register: 1}, zapretReplyEq([]byte{1}),
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 2},
				zapretReplyEq(binary.BigEndian.AppendUint16(nil, port)))
			rule = append(rule, tunMetaEq(expr.MetaKeyMARK, tunU32(0))...)
			var verdict expr.Any = &expr.Verdict{Kind: expr.VerdictAccept}
			if slices.Contains(observe, port) {
				rule = append(rule, tunMetaEq(expr.MetaKeyIIF, tunU32(uint32(egress.IfIndex)))...)
				if address := scope.SourceForFamily(ipv6); address.IsValid() {
					rule = append(rule, &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader,
						Offset: addressOffset, Len: uint32(address.BitLen() / 8)}, zapretReplyEq(address.AsSlice()))
				}
				verdict = &expr.Queue{Num: plan.Queue(), Total: 1}
			}
			// Ordinary TCP profiles in this binding are not sent to the engine.
			// They still require a live owner before their replies can pass.
			rule = append(rule, zapretReplySetMark(plan.QueueMark())...)
			if err := b.addRule(zapretReplyQueue, append(rule, verdict)); err != nil {
				return err
			}
		}
	}
	// Resumed packets must still cross this live table after the queue;
	// entering the queue does not itself authorize delivery to the socket.
	rule := append(zapretTCPSocketMark(plan.SocketMark(), ^uint32(0)), tunMetaEq(expr.MetaKeyMARK, tunU32(plan.QueueMark()))...)
	rule = append(rule, zapretReplySetMark(plan.QueueMark()|plan.FinalRouteBit())...)
	return b.addRule(zapretReplyVerify, append(rule, &expr.Verdict{Kind: expr.VerdictAccept}))
}

func (b *zapretBatch) addZapretReplyDeny() error {
	if err := b.addChain(zapretReplyPrecheck, unix.NF_INET_LOCAL_IN, ZapretReplyPrecheckPriority); err != nil {
		return err
	}
	if err := b.addChain(zapretReplyDeny, unix.NF_INET_LOCAL_IN, ZapretReplyGuardPriority); err != nil {
		return err
	}
	const domain = planner.ZapretSocketMarkBit | planner.ZapretReturnSocketBit
	const marker = domain | planner.ZapretSystemdFinal
	// An skb may already carry its ingress device's mark. Only the live
	// block may generate an approval bit for a selected socket, even after
	// the admission owner disappears. Leave non-selected sockets alone.
	premarked := append(zapretTCPSocketMark(domain, domain), &expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		zapretReplyMask(planner.ZapretSystemdFinal), &expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: tunU32(0)},
		&expr.Verdict{Kind: expr.VerdictDrop})
	if err := b.addRule(zapretReplyPrecheck, premarked); err != nil {
		return err
	}
	approved := append(zapretTCPSocketMark(domain, domain), &expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		zapretReplyMask(marker), zapretReplyEq(tunU32(marker)))
	approved = append(approved, zapretReplySetMark(0)...)
	if err := b.addRule(zapretReplyDeny, append(approved, &expr.Verdict{Kind: expr.VerdictAccept})); err != nil {
		return err
	}
	// Use the socket's mark even when the live table is gone and an arriving
	// reply carries no packet mark. This covers already-connected sockets.
	return b.addRule(zapretReplyDeny, append(zapretTCPSocketMark(domain, domain), &expr.Verdict{Kind: expr.VerdictDrop}))
}

// ZapretReplyDenyHook identifies the two reserved slots to the broker. The
// broker must still verify the actual retained table owner before admission.
func ZapretReplyDenyHook(name string, priority int32) bool {
	return name == zapretReplyPrecheck && priority == ZapretReplyPrecheckPriority ||
		name == zapretReplyDeny && priority == ZapretReplyGuardPriority
}

func zapretTCPSocketMark(mark, mask uint32) []expr.Any {
	rule := append(tunMetaEq(expr.MetaKeyL4PROTO, []byte{unix.IPPROTO_TCP}), &expr.Socket{Key: expr.SocketKeyMark, Register: 1})
	if mask != ^uint32(0) {
		rule = append(rule, zapretReplyMask(mask))
	}
	return append(rule, zapretReplyEq(tunU32(mark)))
}

func zapretReplyMask(mask uint32) *expr.Bitwise {
	return &expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: tunU32(mask), Xor: tunU32(0)}
}

func zapretReplyEq(value []byte) *expr.Cmp {
	return &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: value}
}

func zapretReplySetMark(mark uint32) []expr.Any {
	return []expr.Any{&expr.Immediate{Register: 1, Data: tunU32(mark)}, &expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1}}
}
