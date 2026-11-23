//go:build linux && !minimal

package privilegedbroker

import (
	"encoding/binary"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func TestRetiredZapretTCPIDRequiresExactOrphan(t *testing.T) {
	for _, tc := range []struct {
		name  string
		uid   uint32
		inode uint32
		mark  uint32
		noID  bool
		ok    bool
	}{
		{name: "verified orphan", mark: planner.ZapretSocketMarkBit | 4, ok: true},
		{name: "owned by Panel", uid: 950, mark: planner.ZapretSocketMarkBit | 4},
		{name: "root socket with an open descriptor", inode: 123, mark: planner.ZapretSocketMarkBit | 4},
		{name: "unmarked control"},
		{name: "no mark serial", mark: planner.ZapretSocketMarkBit},
		{name: "no cookie", mark: planner.ZapretSocketMarkBit | 4, noID: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := make([]byte, 72)
			entry[0] = unix.AF_INET
			if !tc.noID {
				entry[44] = 77 // first byte of the kernel cookie
			}
			binary.NativeEndian.PutUint32(entry[64:], tc.uid)
			binary.NativeEndian.PutUint32(entry[68:], tc.inode)
			attributes, err := netlink.MarshalAttributes([]netlink.Attribute{
				{Type: inetDiagMark, Data: binary.NativeEndian.AppendUint32(nil, tc.mark)},
			})
			if err != nil {
				t.Fatal(err)
			}
			id, err := retiredZapretTCPID(append(entry, attributes...), unix.AF_INET)
			if tc.ok && (err != nil || len(id) != 48 || id[40] != 77) {
				t.Fatalf("exact kernel identifier was discarded: %x %v", id, err)
			}
			if !tc.ok && (err == nil || id != nil) {
				t.Fatalf("unrelated socket was admitted for retirement: %x %v", id, err)
			}
		})
	}
}
