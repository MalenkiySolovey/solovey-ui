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
	comment := zapretTableComment("deny:" + bootID)
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
	return createZapretTable(ctx, namespace, planner.ZapretDenyTable, 1, comment, batch.finish())
}

// WithZapretDenyOwner is only used for the broker-to-PID-1 FD store handoff.
func (t *ZapretOwnedTable) WithZapretDenyOwner(use func(int) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil || t.name != planner.ZapretDenyTable {
		return errors.New("Zapret deny owner is unavailable")
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
	inventory, err := readZapretQueueInventory("/proc/net/netlink")
	if err != nil {
		return nil, err
	}
	inode, err := zapretQueueSocketInode(inventory, port)
	if err != nil || inode != expectedInode {
		return nil, errors.New("Zapret deny owner port no longer has the retained descriptor identity")
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
	table := &ZapretOwnedTable{conn: conn, port: port, name: planner.ZapretDenyTable, chains: 1, comment: zapretTableComment("deny:" + bootID)}
	if err := table.setDeadline(ctx); err != nil {
		return nil, err
	}
	table.handle, err = table.observe()
	if err != nil {
		return nil, err
	}
	return table, nil
}
