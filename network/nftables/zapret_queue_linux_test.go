//go:build linux && !minimal

package nftables

import "testing"

func TestZapretQueueInventoryRequiresOnePacketConsumer(t *testing.T) {
	const port uint32 = 4000002751
	const bound = " 27 4000002751 0 2 65531 0 0 36 1\n"
	const other = " 91 90 0 2 65531 0 0 0 1\n"
	if got, err := zapretQueuePort(other+bound, 27); err != nil || got != port {
		t.Fatalf("queue port=%d: %v", got, err)
	}
	for _, bad := range []string{other, bound + bound, "27 0 0 2 1024 0 0 0 1", "27 1 0 1 0 0 0 0 1", "27 1"} {
		if _, err := zapretQueuePort(bad, 27); err == nil {
			t.Fatalf("accepted invalid queue inventory %q", bad)
		}
	}
	const socket = "0000000000000000 12 4000002751 00000000 0 0 0 2 0 235678\n"
	const samePortWrongProtocol = "0000000000000000 0 4000002751 00000000 0 0 0 2 0 235677\n"
	if got, err := zapretQueueSocketInode(samePortWrongProtocol+socket, port); err != nil || got != 235678 {
		t.Fatalf("queue socket inode=%d: %v", got, err)
	}
	for _, bad := range []string{samePortWrongProtocol, socket + socket, "000 12 4000002751 00 0 0 0 0 0 0"} {
		if _, err := zapretQueueSocketInode(bad, port); err == nil {
			t.Fatalf("accepted invalid netlink inventory %q", bad)
		}
	}
}
