//go:build linux && !minimal

package nftables

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	// Linux nf_tables.h attributes not exported by the pinned x/sys.
	nftTableHandle    = 4
	nftTableUserData  = 6
	nftTableOwner     = 7
	nftTableFlagOwner = 2 // Deliberately excludes NFT_TABLE_F_PERSIST.
	nftChainAccept    = 1 // NF_ACCEPT
	zapretOutput      = "local_output"
	zapretPostRouting = "verify_route"
	zapretNFTTimeout  = 2 * time.Second
)

// ZapretOwnedTable holds the only netlink port allowed to change this
// allocation. The kernel removes the table on port close, including broker
// death. It is not a grant; the independent pinned guard must already exist
// and must remain attached after this table disappears.
type ZapretOwnedTable struct {
	mu      sync.Mutex
	conn    *netlink.Conn
	comment []byte
	port    uint32
	handle  uint64
}

// CreateZapretOwnedTable applies exactly one create-only batch for a plan.
// Partial or unconfirmed apply closes its port, never deletes by table name.
func CreateZapretOwnedTable(ctx context.Context, plan planner.ZapretPlan) (_ *ZapretOwnedTable, failure error) {
	scope, err := plan.SocketScope()
	if err != nil || plan.Queue() == 0 || plan.RawPassMark() == 0 {
		return nil, errors.New("missing installed Zapret allocation")
	}
	comment := zapretTableComment(scope.PlanRevision())
	batch, err := zapretOwnedBatch(plan, comment)
	if err != nil {
		return nil, err
	}
	conn, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, fmt.Errorf("open Zapret ruleset owner port: %w", err)
	}
	t := &ZapretOwnedTable{conn: conn, comment: comment}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, conn.Close())
		}
	}()
	if err := t.setDeadline(ctx); err != nil {
		return nil, err
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cookie uint64
	var cookieErr error
	if err := raw.Control(func(fd uintptr) {
		cookie, cookieErr = unix.GetsockoptUint64(int(fd), unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	}); err != nil || cookieErr != nil || cookie != scope.Egress().NamespaceCookie {
		return nil, errors.New("table port for Zapret is not in the planned network namespace")
	}
	sent, err := conn.SendMessages(batch)
	if err != nil {
		return nil, fmt.Errorf("apply Zapret atomic create: %w", err)
	}
	t.port = sent[0].Header.PID
	if err := expectZapretBatchACKs(conn, sent[1:len(sent)-1]); err != nil {
		return nil, fmt.Errorf("apply owner-bound Zapret table (requires nftables table-owner support): %w", err)
	}
	if t.handle, err = t.observe(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *ZapretOwnedTable) Verify(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil || t.handle == 0 {
		return errors.New("table owner for Zapret has retired")
	}
	if err := t.setDeadline(ctx); err != nil {
		return err
	}
	handle, err := t.observe()
	if err != nil {
		return err
	}
	if handle != t.handle {
		return errors.New("table generation for Zapret changed")
	}
	return ctx.Err()
}

func (t *ZapretOwnedTable) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

func (t *ZapretOwnedTable) setDeadline(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(zapretNFTTimeout)
	if stop, ok := ctx.Deadline(); ok && stop.Before(deadline) {
		deadline = stop
	}
	return t.conn.SetDeadline(deadline)
}

func (t *ZapretOwnedTable) observe() (uint64, error) {
	request, err := zapretNFTMessage(unix.NFT_MSG_GETTABLE, 0,
		zapretNftString(unix.NFTA_TABLE_NAME, planner.ZapretInstalledTable))
	if err != nil {
		return 0, err
	}
	replies, err := t.conn.Execute(request)
	if err != nil {
		return 0, fmt.Errorf("observe Zapret kernel table ownership: %w", err)
	}
	if len(replies) != 1 || replies[0].Header.Type != zapretNFTType(unix.NFT_MSG_NEWTABLE) ||
		len(replies[0].Data) < 4 || replies[0].Data[0] != unix.NFPROTO_INET {
		return 0, errors.New("unexpected Zapret table inventory response")
	}
	attrs, err := netlink.NewAttributeDecoder(replies[0].Data[4:])
	if err != nil {
		return 0, err
	}
	attrs.ByteOrder = binary.BigEndian
	var flags, owner, use uint32
	var handle uint64
	var name string
	var comment []byte
	for attrs.Next() {
		switch attrs.Type() {
		case unix.NFTA_TABLE_NAME:
			name = attrs.String()
		case unix.NFTA_TABLE_FLAGS:
			flags = attrs.Uint32()
		case unix.NFTA_TABLE_USE:
			use = attrs.Uint32()
		case nftTableHandle:
			handle = attrs.Uint64()
		case nftTableUserData:
			comment = attrs.Bytes()
		case nftTableOwner:
			owner = attrs.Uint32()
		}
	}
	// This port cannot be used to modify a successful create. A different
	// port cannot edit any owned chains or rules, regardless of their names.
	if attrs.Err() != nil || name != planner.ZapretInstalledTable || flags != nftTableFlagOwner ||
		owner == 0 || owner != t.port || handle == 0 || use != 2 || !bytes.Equal(comment, t.comment) {
		return 0, errors.New("table for Zapret has no exact live owner and plan identity")
	}
	return handle, nil
}

func expectZapretBatchACKs(conn *netlink.Conn, requests []netlink.Message) error {
	pending := make(map[uint32]netlink.Message, len(requests))
	for _, request := range requests {
		pending[request.Header.Sequence] = request
	}
	for len(pending) != 0 {
		replies, err := conn.Receive()
		if err != nil {
			return err
		}
		if len(replies) == 0 {
			return errors.New("empty Zapret nft batch acknowledgment")
		}
		for _, reply := range replies {
			request, ok := pending[reply.Header.Sequence]
			if !ok || reply.Header.Type != netlink.Error || netlink.Validate(request, []netlink.Message{reply}) != nil {
				return errors.New("unexpected Zapret nft batch acknowledgment")
			}
			delete(pending, reply.Header.Sequence)
		}
	}
	return nil
}

func zapretOwnedBatch(plan planner.ZapretPlan, comment []byte) ([]netlink.Message, error) {
	scope, err := plan.SocketScope()
	if err != nil {
		return nil, err
	}
	begin := func(op netlink.HeaderType) netlink.Message {
		return netlink.Message{Header: netlink.Header{Type: op, Flags: netlink.Request},
			Data: []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, unix.NFNL_SUBSYS_NFTABLES}}
	}
	batch := []netlink.Message{begin(unix.NFNL_MSG_BATCH_BEGIN)}
	add := func(op uint8, flags netlink.HeaderFlags, attrs ...netlink.Attribute) error {
		message, err := zapretNFTMessage(op, netlink.Create|netlink.Acknowledge|flags, attrs...)
		if err == nil {
			batch = append(batch, message)
		}
		return err
	}
	if err := add(unix.NFT_MSG_NEWTABLE, netlink.Excl, zapretNftString(unix.NFTA_TABLE_NAME, planner.ZapretInstalledTable),
		zapretNftUint32(unix.NFTA_TABLE_FLAGS, nftTableFlagOwner), netlink.Attribute{Type: nftTableUserData, Data: comment}); err != nil {
		return nil, err
	}
	for _, chain := range []struct {
		name           string
		hook, priority uint32
	}{{zapretOutput, unix.NF_INET_LOCAL_OUT, 150}, {zapretPostRouting, unix.NF_INET_POST_ROUTING, zapretGuardPriority - 1}} {
		hook, err := netlink.MarshalAttributes([]netlink.Attribute{
			zapretNftUint32(unix.NFTA_HOOK_HOOKNUM, chain.hook), zapretNftUint32(unix.NFTA_HOOK_PRIORITY, chain.priority),
		})
		if err != nil {
			return nil, err
		}
		if err := add(unix.NFT_MSG_NEWCHAIN, netlink.Excl, zapretNftString(unix.NFTA_CHAIN_TABLE, planner.ZapretInstalledTable),
			zapretNftString(unix.NFTA_CHAIN_NAME, chain.name), zapretNftString(unix.NFTA_CHAIN_TYPE, "filter"),
			zapretNftUint32(unix.NFTA_CHAIN_POLICY, nftChainAccept), netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_CHAIN_HOOK, Data: hook}); err != nil {
			return nil, err
		}
	}
	addRule := func(chain string, expressions []expr.Any) error {
		attrs := make([]netlink.Attribute, 0, len(expressions))
		for _, value := range expressions {
			encoded, err := expr.Marshal(unix.NFPROTO_INET, value)
			if err != nil {
				return err
			}
			attrs = append(attrs, netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_LIST_ELEM, Data: encoded})
		}
		data, err := netlink.MarshalAttributes(attrs)
		if err != nil {
			return err
		}
		return add(unix.NFT_MSG_NEWRULE, netlink.Append, zapretNftString(unix.NFTA_RULE_TABLE, planner.ZapretInstalledTable),
			zapretNftString(unix.NFTA_RULE_CHAIN, chain), netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_RULE_EXPRESSIONS, Data: data})
	}
	const reg = unix.NFT_REG_1
	meta := func(key uint32) expr.Any { return &expr.Meta{Key: expr.MetaKey(key), Register: reg} }
	eq := func(data []byte) expr.Any { return &expr.Cmp{Op: expr.CmpOpEq, Register: reg, Data: data} }
	u32 := func(value uint32) []byte { return binary.NativeEndian.AppendUint32(nil, value) }
	setMark := func(value uint32) []expr.Any {
		return []expr.Any{&expr.Immediate{Register: reg, Data: u32(value)}, &expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: reg}}
	}
	egress := scope.Egress()
	packet := plan.PacketScope()
	generated := []uint32{plan.LoopMark(), plan.LoopMark() | plan.QueueMark()}
	for _, route := range []struct {
		family  byte
		gateway netip.Addr
		key     expr.RtKey
	}{{unix.NFPROTO_IPV4, egress.IPv4Gateway, expr.RtNexthop4}, {unix.NFPROTO_IPV6, egress.IPv6Gateway, expr.RtNexthop6}} {
		if !route.gateway.IsValid() {
			continue
		}
		path := []expr.Any{meta(unix.NFT_META_NFPROTO), eq([]byte{route.family}),
			meta(unix.NFT_META_OIF), eq(u32(uint32(egress.IfIndex))),
			&expr.Rt{Key: route.key, Register: reg}, eq(route.gateway.AsSlice())}
		for _, protocol := range packet.Protocols {
			l4, ports := byte(unix.IPPROTO_TCP), packet.TCPPorts
			if protocol == intent.PacketProtocolUDP {
				l4, ports = unix.IPPROTO_UDP, packet.UDPPorts
			}
			for _, port := range ports {
				match := append(append([]expr.Any(nil), path...), meta(unix.NFT_META_L4PROTO), eq([]byte{l4}),
					&expr.Payload{DestRegister: reg, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					eq(binary.BigEndian.AppendUint16(nil, port)))
				original := append(append([]expr.Any(nil), match...), &expr.Ct{Key: expr.CtKeyDIRECTION, Register: reg}, eq([]byte{0}),
					meta(unix.NFT_META_MARK), eq(u32(plan.SocketMark())))
				original = append(original, setMark(plan.QueueMark())...)
				if err := addRule(zapretOutput, append(original, &expr.Queue{Num: plan.Queue(), Total: 1})); err != nil {
					return nil, err
				}
				for _, mark := range generated {
					raw := append(append([]expr.Any(nil), match...), meta(unix.NFT_META_MARK), eq(u32(mark)))
					raw = append(raw, setMark(mark|plan.RawPassMark())...)
					if err := addRule(zapretOutput, append(raw, &expr.Verdict{Kind: expr.VerdictAccept})); err != nil {
						return nil, err
					}
				}
			}
		}
		// A later route-type OUTPUT chain can cause a second lookup after
		// NFQUEUE reinjection. Require the actual route again in POSTROUTING.
		for _, mark := range []uint32{plan.QueueMark(), generated[0] | plan.RawPassMark(), generated[1] | plan.RawPassMark()} {
			match := append(append([]expr.Any(nil), path...), meta(unix.NFT_META_MARK), eq(u32(mark)), &expr.Verdict{Kind: expr.VerdictAccept})
			if err := addRule(zapretPostRouting, match); err != nil {
				return nil, err
			}
		}
	}
	for _, chain := range []string{zapretOutput, zapretPostRouting} {
		if err := addRule(chain, []expr.Any{meta(unix.NFT_META_MARK),
			&expr.Bitwise{SourceRegister: reg, DestRegister: reg, Len: 4, Mask: u32(plan.QueueMark() | plan.LoopMark() | plan.RawPassMark()), Xor: u32(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: reg, Data: u32(0)}, &expr.Verdict{Kind: expr.VerdictDrop},
		}); err != nil {
			return nil, err
		}
	}
	return append(batch, begin(unix.NFNL_MSG_BATCH_END)), nil
}

func zapretTableComment(revision string) []byte {
	text := "solovey-r8b1:" + revision
	return append(append([]byte{0, byte(len(text) + 1)}, text...), 0)
}

func zapretNFTType(op uint8) netlink.HeaderType {
	return netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | uint16(op))
}

func zapretNFTMessage(op uint8, flags netlink.HeaderFlags, attrs ...netlink.Attribute) (netlink.Message, error) {
	data, err := netlink.MarshalAttributes(attrs)
	return netlink.Message{Header: netlink.Header{Type: zapretNFTType(op), Flags: netlink.Request | flags},
		Data: append([]byte{unix.NFPROTO_INET, unix.NFNETLINK_V0, 0, 0}, data...)}, err
}

func zapretNftString(typ uint16, value string) netlink.Attribute {
	return netlink.Attribute{Type: typ, Data: append([]byte(value), 0)}
}

func zapretNftUint32(typ uint16, value uint32) netlink.Attribute {
	return netlink.Attribute{Type: typ, Data: binary.BigEndian.AppendUint32(nil, value)}
}
