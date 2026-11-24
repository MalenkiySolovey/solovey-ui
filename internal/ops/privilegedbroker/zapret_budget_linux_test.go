//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

func TestZapretBindingDescriptorCapacityAccountsForActiveWorkersAndTUN(t *testing.T) {
	const used, bindings = 20, 6
	limit := uint64(used + zapretBatchFDs + bindings*zapretBindingFDs)
	if got := zapretBindingDescriptorCapacity(limit, used, false); got != bindings {
		t.Fatalf("%d descriptors should admit %d bindings, got %d", limit, bindings, got)
	}
	for _, test := range []struct {
		limit, used, want uint64
		tun               bool
	}{
		{limit - 1, used, bindings - 1, false},
		{limit, used, bindings - 1, true},
		{limit + zapretTUNFDs, used, bindings, true},
		{limit, limit + 1, 0, false},
		{used + zapretBatchFDs - 1, used, 0, false},
	} {
		if got := zapretBindingDescriptorCapacity(test.limit, test.used, test.tun); got != test.want {
			t.Fatalf("capacity for %+v = %d", test, got)
		}
	}
}

func TestZapretMarkPreviewDoesNotSpendSerials(t *testing.T) {
	const left = 5
	used := uint64(planner.ZapretMaxSerial - left)
	j := &FileJournal{bootID: "boot", fences: map[string]uint64{zapretSystemdResource: used}, sequence: 23}
	peer := PeerIdentity{BootID: j.bootID, Revision: strings.Repeat("a", 64)}
	for range 2 {
		if got, err := j.previewZapretMarkBatch(peer, left); err != nil || got != uint32(used)+1 {
			t.Fatalf("cannot preview %d remaining marks: %d, %v", left, got, err)
		}
	}
	if _, err := j.previewZapretMarkBatch(peer, left+1); !errors.Is(err, errZapretMarkBudget) {
		t.Fatalf("preview accepted exhausted serial space: %v", err)
	}
	if j.fences[zapretSystemdResource] != used || j.sequence != 23 {
		t.Fatal("read-only validation consumed journal state")
	}
}
