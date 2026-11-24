//go:build linux && !minimal

package nftables

import (
	"errors"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func TestZapretNetlinkBudgetCountsAlignedMessagesAndBoundaries(t *testing.T) {
	batch := []netlink.Message{
		zapretBatchBoundary(unix.NFNL_MSG_BATCH_BEGIN),
		{Data: []byte{1, 2, 3, 4, 5}},
		zapretBatchBoundary(unix.NFNL_MSG_BATCH_END),
	}
	got := zapretNetlinkBatchBytes(batch)
	if got != 3*unix.NLMSG_HDRLEN+4+8+4 {
		t.Fatalf("unexpected aligned transaction size %d", got)
	}
	if err := validateZapretNetlinkSize(got, got+32); err != nil {
		t.Fatalf("rejected a transaction that fits: %v", err)
	}
	if err := validateZapretNetlinkSize(got, got+31); !errors.Is(err, ErrZapretAtomicBudget) {
		t.Fatalf("oversized transaction was not rejected: %v", err)
	}
}
