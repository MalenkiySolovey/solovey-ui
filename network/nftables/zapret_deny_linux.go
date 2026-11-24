//go:build linux && !minimal

package nftables

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// CreateZapretDenyTable independently protects already-issued marks. The
// caller must give PID 1 a retained copy of this owner port before allowing
// any plan to mark sockets. No live queue or engine is needed to deny.
func CreateZapretDenyTable(ctx context.Context, namespace uint64, bootID string) (*ZapretOwnedTable, error) {
	return CreateZapretDenyTableForIngress(ctx, namespace, bootID, nil, false)
}

func CreateZapretDenyTableForIngress(ctx context.Context, namespace uint64, bootID string, tun *planner.ZapretTUNScope, replies bool) (*ZapretOwnedTable, error) {
	comment, chains := zapretDenyShape(bootID, tun, replies)
	const chain = "deny_unadmitted"
	batch, err := newZapretBatch(planner.ZapretDenyTable, comment)
	if err != nil {
		return nil, err
	}
	if err := batch.addChain(chain, unix.NF_INET_POST_ROUTING, zapretGuardPriority); err != nil {
		return nil, err
	}
	const reg = unix.NFT_REG_1
	u32 := func(n uint32) []byte { return binary.NativeEndian.AppendUint32(nil, n) }
	// Only the live admission table's postrouting rules mint the final bit.
	if err := batch.addRule(chain, []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: reg},
		&expr.Bitwise{SourceRegister: reg, DestRegister: reg, Len: 4, Mask: u32(planner.ZapretSystemdFinal), Xor: u32(0)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: reg, Data: u32(0)},
		&expr.Meta{Key: expr.MetaKeyMARK, Register: reg},
		&expr.Bitwise{SourceRegister: reg, DestRegister: reg, Len: 4, Mask: u32(planner.ZapretDenyMask), Xor: u32(0)},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: reg, Data: u32(0)},
		&expr.Verdict{Kind: expr.VerdictDrop},
	}); err != nil {
		return nil, err
	}
	if tun != nil {
		if err := batch.addZapretTUNDeny(*tun); err != nil {
			return nil, err
		}
	}
	if replies {
		if err := batch.addZapretReplyDeny(); err != nil {
			return nil, err
		}
	}
	return createZapretTable(ctx, namespace, planner.ZapretDenyTable, chains, comment, batch.finish())
}

func zapretDenyShape(bootID string, tun *planner.ZapretTUNScope, replies bool) ([]byte, uint32) {
	value, chains := "deny:"+bootID, uint32(1)
	if tun != nil {
		value, chains = "deny-tun:"+tun.Revision(bootID), 3
	}
	if replies {
		value, chains = value+":input-v1", chains+2
	}
	return zapretTableComment(value), chains
}

// WithZapretDenyOwner is only used for the broker-to-PID-1 FD store handoff.
func (t *ZapretOwnedTable) WithZapretDenyOwner(use func(int) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil || t.name != planner.ZapretDenyTable {
		return errors.New("the Zapret deny owner is unavailable")
	}
	raw, err := t.conn.SyscallConn()
	if err != nil {
		return err
	}
	var useErr error
	return errors.Join(raw.Control(func(fd uintptr) { useErr = use(int(fd)) }), useErr)
}

// ObserveZapretDenyTable opens a read-only witness for a deny whose owner
// descriptor was previously verified in the systemd FD store. This connection
// cannot modify the table and its close does not retire the owner.
func ObserveZapretDenyTable(ctx context.Context, namespace uint64, bootID string, port uint32, expectedInode uint64) (_ *ZapretOwnedTable, failure error) {
	return ObserveZapretDenyTableForIngress(ctx, namespace, bootID, port, expectedInode, nil, false)
}

func ObserveZapretDenyTableForIngress(ctx context.Context, namespace uint64, bootID string, port uint32, expectedInode uint64, tun *planner.ZapretTUNScope, replies bool) (_ *ZapretOwnedTable, failure error) {
	inventory, err := readZapretQueueInventory("/proc/net/netlink")
	if err != nil {
		return nil, err
	}
	inode, err := zapretQueueSocketInode(inventory, port)
	if err != nil || inode != expectedInode {
		return nil, errors.New("the Zapret deny owner port no longer has the retained descriptor identity")
	}
	conn, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, conn.Close())
		}
	}()
	if err := checkZapretTableNamespace(conn, namespace); err != nil {
		return nil, err
	}
	comment, chains := zapretDenyShape(bootID, tun, replies)
	table := &ZapretOwnedTable{conn: conn, port: port, name: planner.ZapretDenyTable, chains: chains, comment: comment}
	if err := table.setDeadline(ctx); err != nil {
		return nil, err
	}
	table.handle, err = table.observe()
	if err != nil {
		return nil, err
	}
	return table, nil
}
