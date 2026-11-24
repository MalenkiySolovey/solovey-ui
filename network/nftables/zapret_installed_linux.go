//go:build linux && !minimal

package nftables

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	nftTableHandle      = 4
	nftTableUserData    = 6
	nftTableOwner       = 7
	nftTableFlagOwner   = 2 // Deliberately excludes NFT_TABLE_F_PERSIST.
	nftChainAccept      = 1 // NF_ACCEPT
	zapretOutput        = "local_output"
	zapretPostRouting   = "verify_route"
	zapretNFTTimeout    = 2 * time.Second
	zapretGuardPriority = math.MaxInt32 - 1
)

var ErrZapretAtomicBudget = errors.New("installed Zapret nftables atomic batch exceeds the netlink socket budget")

// ZapretOwnedTable holds the only netlink port allowed to change this
// allocation. The kernel removes the table on port close, including broker
// death. It is not a grant; the independent deny must already exist and
// must remain attached after this table disappears.
type ZapretOwnedTable struct {
	mu      sync.Mutex
	conn    *netlink.Conn
	name    string
	chains  uint32
	comment []byte
	port    uint32
	handle  uint64
	bytes   int
}

func (t *ZapretOwnedTable) BatchBytes() int { return t.bytes }

// CreateZapretOwnedTable applies exactly one create-only batch for a plan.
// Partial or unconfirmed apply closes its port, never deletes by table name.
func CreateZapretOwnedTable(ctx context.Context, plan planner.ZapretPlan) (_ *ZapretOwnedTable, failure error) {
	return CreateZapretOwnedBatch(ctx, []planner.ZapretPlan{plan})
}

// CreateZapretOwnedBatch installs all protected selectors atomically, under
// one owner port. No partial live table exists if any rule cannot be installed.
func CreateZapretOwnedBatch(ctx context.Context, plans []planner.ZapretPlan) (*ZapretOwnedTable, error) {
	return CreateZapretOwnedBatchForIngress(ctx, plans, nil)
}

func CreateZapretOwnedBatchForIngress(ctx context.Context, plans []planner.ZapretPlan, tun *planner.ZapretTUNForward) (*ZapretOwnedTable, error) {
	draft, err := buildZapretBatch(plans, tun)
	if err != nil {
		return nil, err
	}
	return createZapretTable(ctx, draft.namespace, planner.ZapretInstalledTable, draft.chains, draft.comment, draft.messages)
}

type zapretTableDraft struct {
	namespace uint64
	chains    uint32
	comment   []byte
	messages  []netlink.Message
}

// CheckZapretOwnedBatchBudget serializes the intended rules against the local
// socket limit without sending a transaction or opening a table owner.
func CheckZapretOwnedBatchBudget(ctx context.Context, plans []planner.ZapretPlan, tun *planner.ZapretTUNForward) error {
	draft, err := buildZapretBatch(plans, tun)
	if err != nil {
		return err
	}
	conn, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := checkZapretTableNamespace(conn, draft.namespace); err != nil {
		return err
	}
	if err := checkZapretNetlinkBudget(conn, draft.messages); err != nil {
		return err
	}
	return ctx.Err()
}

func buildZapretBatch(plans []planner.ZapretPlan, tun *planner.ZapretTUNForward) (zapretTableDraft, error) {
	if len(plans) == 0 {
		return zapretTableDraft{}, errors.New("missing installed Zapret allocations")
	}
	if tun != nil && !tun.Valid() {
		return zapretTableDraft{}, errors.New("invalid Zapret TUN forwarding plan")
	}
	plan := plans[0]
	scope, err := plan.SocketScope()
	if err != nil || plan.Queue() == 0 || plan.FinalRouteBit() == 0 {
		return zapretTableDraft{}, errors.New("missing installed Zapret allocation")
	}
	comment := zapretTableComment(scope.PlanRevision())
	if len(plans) > 1 {
		var revisions []byte
		marks := make(map[uint32]bool, len(plans))
		for _, member := range plans {
			s, err := member.SocketScope()
			if err != nil || s.Egress() != scope.Egress() || member.Queue() != plan.Queue() ||
				member.LoopMark() != plan.LoopMark() || member.FinalRouteBit() != plan.FinalRouteBit() ||
				member.PacketMarkMask() != plan.PacketMarkMask() || marks[member.SocketMark()] {
				return zapretTableDraft{}, errors.New("inconsistent installed Zapret batch allocation")
			}
			marks[member.SocketMark()] = true
			revisions = append(revisions, s.PlanRevision()...)
		}
		comment = zapretTableComment(fmt.Sprintf("%x", sha256.Sum256(revisions)))
	}
	batch, err := zapretOwnedBatch(plans, comment, tun)
	if err != nil {
		return zapretTableDraft{}, err
	}
	chains := uint32(2)
	if tun != nil {
		chains++
	}
	return zapretTableDraft{scope.Egress().NamespaceCookie, chains, comment, batch}, nil
}

func createZapretTable(ctx context.Context, namespace uint64, name string, chains uint32, comment []byte, batch []netlink.Message) (_ *ZapretOwnedTable, failure error) {
	conn, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, fmt.Errorf("open Zapret ruleset owner port: %w", err)
	}
	t := &ZapretOwnedTable{conn: conn, name: name, chains: chains, comment: comment}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, conn.Close())
		}
	}()
	if err := t.setDeadline(ctx); err != nil {
		return nil, err
	}
	if err := checkZapretTableNamespace(conn, namespace); err != nil {
		return nil, err
	}
	if err := checkZapretNetlinkBudget(conn, batch); err != nil {
		return nil, err
	}
	t.bytes = zapretNetlinkBatchBytes(batch)
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

func checkZapretNetlinkBudget(conn *netlink.Conn, batch []netlink.Message) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var sendBuffer, receiveBuffer int
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		sendBuffer, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF)
		if socketErr == nil {
			receiveBuffer, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
		}
	}); err != nil || socketErr != nil {
		return errors.Join(err, socketErr)
	}
	size := zapretNetlinkBatchBytes(batch)
	if err := validateZapretNetlinkSize(size, sendBuffer); err != nil {
		return err
	}
	// The kernel doubles the buffer request. Never shrink the existing ACK
	// queue, and never force a value above the host's configured rmem maximum.
	if size > receiveBuffer/2 {
		return conn.SetReadBuffer(size)
	}
	return nil
}

func zapretNetlinkBatchBytes(batch []netlink.Message) int {
	var bytes int
	for _, message := range batch {
		bytes += (unix.NLMSG_HDRLEN + len(message.Data) + 3) &^ 3
	}
	return bytes
}

func validateZapretNetlinkSize(bytes, sendBuffer int) error {
	// SendMessages sends one datagram. Linux netlink_sendmsg reserves 32
	// bytes of SO_SNDBUF. Messages and the batch boundaries are 4-aligned.
	if limit := sendBuffer - 32; bytes > limit {
		return fmt.Errorf("%w: %d bytes exceed %d; reduce selected ports or bindings", ErrZapretAtomicBudget, bytes, limit)
	}
	return nil
}

func checkZapretTableNamespace(conn *netlink.Conn, namespace uint64) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var cookie uint64
	var cookieErr error
	if err := raw.Control(func(fd uintptr) {
		cookie, cookieErr = unix.GetsockoptUint64(int(fd), unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	}); err != nil || cookieErr != nil || cookie != namespace {
		return errors.New("table port for Zapret is not in the planned network namespace")
	}
	return nil
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
		zapretNftString(unix.NFTA_TABLE_NAME, t.name))
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
	if attrs.Err() != nil || name != t.name || flags != nftTableFlagOwner ||
		owner == 0 || owner != t.port || handle == 0 || use != t.chains || !bytes.Equal(comment, t.comment) {
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

func zapretOwnedBatch(plans []planner.ZapretPlan, comment []byte, tun ...*planner.ZapretTUNForward) ([]netlink.Message, error) {
	batch, err := newZapretBatch(planner.ZapretInstalledTable, comment)
	if err != nil {
		return nil, err
	}
	if err := batch.addChain(zapretOutput, unix.NF_INET_LOCAL_OUT, 150); err != nil {
		return nil, err
	}
	if err := batch.addChain(zapretPostRouting, unix.NF_INET_POST_ROUTING, zapretGuardPriority-1); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if err := batch.addZapretPath(plan); err != nil {
			return nil, err
		}
	}
	const reg = unix.NFT_REG_1
	u32 := func(value uint32) []byte { return binary.NativeEndian.AppendUint32(nil, value) }
	for _, chain := range []string{zapretOutput, zapretPostRouting} {
		if err := batch.addRule(chain, []expr.Any{&expr.Meta{Key: expr.MetaKeyMARK, Register: reg},
			&expr.Bitwise{SourceRegister: reg, DestRegister: reg, Len: 4, Mask: u32(plans[0].PacketMarkMask()), Xor: u32(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: reg, Data: u32(0)}, &expr.Verdict{Kind: expr.VerdictDrop},
		}); err != nil {
			return nil, err
		}
	}
	if len(tun) == 1 && tun[0] != nil {
		if err := batch.addZapretTUNForward(tun[0]); err != nil {
			return nil, err
		}
	}
	return batch.finish(), nil
}

func (batch *zapretBatch) addZapretPath(plan planner.ZapretPlan) error {
	scope, err := plan.SocketScope()
	if err != nil {
		return err
	}
	addRule := batch.addRule
	const reg = unix.NFT_REG_1
	meta := func(key uint32) expr.Any { return &expr.Meta{Key: expr.MetaKey(key), Register: reg} }
	socketMark := &expr.Socket{Key: expr.SocketKeyMark, Register: reg}
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
		ipv6 := route.family == unix.NFPROTO_IPV6
		originalPath := path
		if source := scope.SourceForFamily(ipv6); source.IsValid() {
			offset := uint32(12)
			if ipv6 {
				offset = 8
			}
			originalPath = append(append([]expr.Any(nil), path...),
				&expr.Payload{DestRegister: reg, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: uint32(source.BitLen() / 8)},
				eq(source.AsSlice()),
				&expr.Fib{Register: reg, ResultADDRTYPE: true, FlagSADDR: true, FlagOIF: true}, eq(u32(unix.RTN_LOCAL)))
		}
		for _, protocol := range packet.Protocols {
			l4 := byte(unix.IPPROTO_TCP)
			if protocol == intent.PacketProtocolUDP {
				l4 = unix.IPPROTO_UDP
			}
			for _, port := range plan.PortsForFamily(protocol, ipv6) {
				match := append(append([]expr.Any(nil), originalPath...), meta(unix.NFT_META_L4PROTO), eq([]byte{l4}),
					&expr.Payload{DestRegister: reg, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					eq(binary.BigEndian.AppendUint16(nil, port)))
				// A foreign packet-only mark cannot put another socket in our queue.
				original := append(append([]expr.Any(nil), match...), &expr.Ct{Key: expr.CtKeyDIRECTION, Register: reg}, eq([]byte{0}),
					meta(unix.NFT_META_MARK), eq(u32(plan.SocketMark())), socketMark, eq(u32(plan.SocketMark())))
				original = append(original, setMark(plan.QueueMark())...)
				if err := addRule(zapretOutput, append(original, &expr.Queue{Num: plan.Queue(), Total: 1})); err != nil {
					return err
				}
				for _, mark := range generated {
					raw := append(append([]expr.Any(nil), match...), meta(unix.NFT_META_MARK), eq(u32(mark)), socketMark, eq(u32(mark)))
					raw = append(raw, setMark(plan.RawOutputMark())...)
					if err := addRule(zapretOutput, append(raw, &expr.Verdict{Kind: expr.VerdictAccept})); err != nil {
						return err
					}
				}
			}
		}
		// NFQUEUE admission is not final egress approval. A later OUTPUT
		// lookup can change the next hop, or the owned port can disappear
		// while a packet is between the two hooks. Mint the final bit here.
		for _, mark := range []uint32{plan.QueueMark(), plan.RawOutputMark()} {
			checkPath := path
			if mark == plan.QueueMark() {
				checkPath = originalPath
			}
			match := append(append([]expr.Any(nil), checkPath...), meta(unix.NFT_META_MARK), eq(u32(mark)))
			match = append(match, setMark(mark|plan.FinalRouteBit())...)
			if err := addRule(zapretPostRouting, append(match, &expr.Verdict{Kind: expr.VerdictAccept})); err != nil {
				return err
			}
		}
	}
	return nil
}

type zapretBatch struct {
	name     string
	messages []netlink.Message
}

func newZapretBatch(name string, comment []byte) (*zapretBatch, error) {
	b := &zapretBatch{name: name, messages: []netlink.Message{zapretBatchBoundary(unix.NFNL_MSG_BATCH_BEGIN)}}
	err := b.add(unix.NFT_MSG_NEWTABLE, netlink.Excl, zapretNftString(unix.NFTA_TABLE_NAME, name),
		zapretNftUint32(unix.NFTA_TABLE_FLAGS, nftTableFlagOwner), netlink.Attribute{Type: nftTableUserData, Data: comment})
	return b, err
}

func zapretBatchBoundary(op netlink.HeaderType) netlink.Message {
	return netlink.Message{Header: netlink.Header{Type: op, Flags: netlink.Request}, Data: []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, unix.NFNL_SUBSYS_NFTABLES}}
}

func (b *zapretBatch) add(op uint8, flags netlink.HeaderFlags, attrs ...netlink.Attribute) error {
	message, err := zapretNFTMessage(op, netlink.Create|netlink.Acknowledge|flags, attrs...)
	if err == nil {
		b.messages = append(b.messages, message)
	}
	return err
}

func (b *zapretBatch) addChain(name string, hookNum uint32, priority int32) error {
	hook, err := netlink.MarshalAttributes([]netlink.Attribute{
		zapretNftUint32(unix.NFTA_HOOK_HOOKNUM, hookNum), zapretNftUint32(unix.NFTA_HOOK_PRIORITY, uint32(priority)),
	})
	if err != nil {
		return err
	}
	return b.add(unix.NFT_MSG_NEWCHAIN, netlink.Excl, zapretNftString(unix.NFTA_CHAIN_TABLE, b.name),
		zapretNftString(unix.NFTA_CHAIN_NAME, name), zapretNftString(unix.NFTA_CHAIN_TYPE, "filter"),
		zapretNftUint32(unix.NFTA_CHAIN_POLICY, nftChainAccept), netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_CHAIN_HOOK, Data: hook})
}

func (b *zapretBatch) addRule(chain string, expressions []expr.Any) error {
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
	return b.add(unix.NFT_MSG_NEWRULE, netlink.Append, zapretNftString(unix.NFTA_RULE_TABLE, b.name),
		zapretNftString(unix.NFTA_RULE_CHAIN, chain), netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_RULE_EXPRESSIONS, Data: data})
}

func (b *zapretBatch) finish() []netlink.Message {
	return append(b.messages, zapretBatchBoundary(unix.NFNL_MSG_BATCH_END))
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
