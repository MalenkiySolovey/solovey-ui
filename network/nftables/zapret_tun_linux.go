//go:build linux && !minimal

package nftables

import (
	"encoding/binary"
	"net/netip"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const zapretTUNAllowPriority = -150

func (b *zapretBatch) addZapretTUNDeny(scope planner.ZapretTUNScope) error {
	const input, forward = "deny_tun_input", "deny_retired_tun"
	if err := b.addChain(input, unix.NF_INET_LOCAL_IN, zapretTUNAllowPriority+1); err != nil {
		return err
	}
	if err := b.addChain(forward, unix.NF_INET_FORWARD, zapretTUNAllowPriority+1); err != nil {
		return err
	}
	name := make([]byte, unix.IFNAMSIZ)
	copy(name, planner.ZapretTUNName)
	ingressName := make([]byte, unix.IFNAMSIZ)
	copy(ingressName, scope.Ingress)
	client := netip.PrefixFrom(scope.Client, 32)
	scopedIngress := func() []expr.Any {
		return append(tunEndpoints(client, scope.Destination), tunMetaEq(expr.MetaKeyIIFNAME, ingressName)...)
	}
	for _, match := range [][]expr.Any{tunMetaEq(expr.MetaKeyIIFNAME, name), scopedIngress()} {
		// The standard local RPDB rule precedes the TUN lookup. A newly
		// local destination must not redirect this ingress to a host socket.
		if err := b.addRule(input, append(match, &expr.Verdict{Kind: expr.VerdictDrop})); err != nil {
			return err
		}
	}
	for _, match := range [][]expr.Any{
		tunMetaEq(expr.MetaKeyIIFNAME, name),
		tunMetaEq(expr.MetaKeyOIFNAME, name),
		scopedIngress(),
	} {
		rule := append(match, &expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: tunU32(planner.ZapretTUNForwardBit), Xor: tunU32(0)},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: tunU32(0)}, &expr.Verdict{Kind: expr.VerdictDrop})
		if err := b.addRule(forward, rule); err != nil {
			return err
		}
	}
	return nil
}

func (b *zapretBatch) addZapretTUNForward(tun *planner.ZapretTUNForward) error {
	const chain = "scoped_tun_forward"
	if err := b.addChain(chain, unix.NF_INET_FORWARD, zapretTUNAllowPriority); err != nil {
		return err
	}
	scope, ingress, tunIndex := tun.Scope, tun.IngressIfIndex, tun.DeviceIfIndex
	client := netip.PrefixFrom(scope.Client, 32)
	name := make([]byte, unix.IFNAMSIZ)
	copy(name, scope.Ingress)
	device := make([]byte, unix.IFNAMSIZ)
	copy(device, planner.ZapretTUNName)
	for _, path := range []struct {
		input, output   int
		source, target  netip.Prefix
		inName, outName []byte
	}{
		{ingress, tunIndex, client, scope.Destination, name, device},
		{tunIndex, ingress, scope.Destination, client, device, name},
	} {
		for _, protocol := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			rule := tunEndpoints(path.source, path.target)
			rule = append(rule, tunMetaEq(expr.MetaKeyIIF, tunU32(uint32(path.input)))...)
			rule = append(rule, tunMetaEq(expr.MetaKeyOIF, tunU32(uint32(path.output)))...)
			rule = append(rule, tunMetaEq(expr.MetaKeyIIFNAME, path.inName)...)
			rule = append(rule, tunMetaEq(expr.MetaKeyOIFNAME, path.outName)...)
			rule = append(rule, tunMetaEq(expr.MetaKeyL4PROTO, []byte{protocol})...)
			if path.input == tunIndex {
				rule = append(rule, &expr.Ct{Key: expr.CtKeySTATE, Register: 1},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: tunU32(expr.CtStateBitESTABLISHED), Xor: tunU32(0)},
					&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: tunU32(0)})
			}
			// Mint the path bit only for this ingress/return scope, then stop
			// this chain before the alternate-route deny. Other base chains
			// still run. No packet here obtains a Zapret socket/queue mark.
			rule = append(rule, &expr.Immediate{Register: 1, Data: tunU32(planner.ZapretTUNForwardBit)},
				&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1}, &expr.Verdict{Kind: expr.VerdictAccept})
			if err := b.addRule(chain, rule); err != nil {
				return err
			}
		}
	}
	// Even if a route changes while the owner is live, the original client
	// packet cannot be forwarded to a different device. After broker loss
	// the independent terminal prohibit and retained device deny take over.
	byName := append(tunEndpoints(client, scope.Destination), tunMetaEq(expr.MetaKeyIIFNAME, name)...)
	byIndex := append(tunEndpoints(client, scope.Destination), tunMetaEq(expr.MetaKeyIIF, tunU32(uint32(ingress)))...)
	for _, rule := range [][]expr.Any{byName, byIndex} {
		if err := b.addRule(chain, append(rule, &expr.Verdict{Kind: expr.VerdictDrop})); err != nil {
			return err
		}
	}
	return nil
}

func tunEndpoints(source, destination netip.Prefix) []expr.Any {
	rule := tunMetaEq(expr.MetaKeyNFPROTO, []byte{unix.NFPROTO_IPV4})
	for i, prefix := range []netip.Prefix{source, destination} {
		mask := ^uint32(0) << (32 - prefix.Bits())
		rule = append(rule, &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: uint32(12 + 4*i), Len: 4},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binary.BigEndian.AppendUint32(nil, mask), Xor: tunU32(0)},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: prefix.Addr().AsSlice()})
	}
	return rule
}

func tunMetaEq(key expr.MetaKey, data []byte) []expr.Any {
	return []expr.Any{&expr.Meta{Key: key, Register: 1}, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: data}}
}

func tunU32(value uint32) []byte { return binary.NativeEndian.AppendUint32(nil, value) }
