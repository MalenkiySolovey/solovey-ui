//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
)

func TestZapretSocketCommitJournal(t *testing.T) {
	root := testJournalRoot(t)
	plan := testZapretSocketPlan(t, 55, 17)
	scope, err := plan.SocketScope()
	if err != nil {
		t.Fatal(err)
	}
	grant := ZapretSocketGrant{Scope: scope, Generation: Digest([]byte("generation")), PeerRevision: Digest([]byte("attested-peer"))}
	checkpoint := nftables.ZapretPathCheckpoint{Generation: grant.Generation, PlanRevision: scope.PlanRevision(), Egress: scope.Egress(),
		SocketMark: plan.SocketMark(), QueueMark: plan.QueueMark(), LoopMark: plan.LoopMark(), RawPassMark: plan.RawPassMark(),
		TablePort: 77, TableHandle: 23, GuardLinks: [2]uint32{4, 5}, QueueOwner: nftables.ZapretQueueOwner{
			Queue: plan.Queue(), PortID: 88, SocketInode: 204, ProcessRevision: Digest([]byte("engine")),
		}}
	journal, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.checkZapretPath(grant); err == nil {
		t.Fatal("grant without journal authority was accepted")
	}
	if err := journal.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.checkZapretPath(grant); err != nil {
		t.Fatalf("reopened journal did not retain exact authority: %v", err)
	}
	newGeneration := grant
	newGeneration.Generation = Digest([]byte("next-generation"))
	newPeer := grant
	newPeer.PeerRevision = Digest([]byte("new-panel-pid"))
	for _, wrong := range []ZapretSocketGrant{newGeneration, newPeer} {
		if err := reopened.checkZapretPath(wrong); err == nil {
			t.Fatalf("receipt accepted mismatched authority: %+v", wrong)
		}
	}
	oldBoot, err := openTestJournal(root, "boot-two")
	if err != nil {
		t.Fatal(err)
	}
	if err := oldBoot.checkZapretPath(grant); err == nil {
		t.Fatal("grant accepted a previous boot's authority")
	}
	checkpoint.Generation = newGeneration.Generation
	if err := reopened.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); publicCode(err) != CodeRecoveryRequired {
		t.Fatalf("undrained generation did not fence replacement: %v", err)
	}

	t.Run("failed completion sync is not a successful commit", func(t *testing.T) {
		journal, err := openTestJournal(testJournalRoot(t), "boot-one")
		if err != nil {
			t.Fatal(err)
		}
		want := errors.New("injected directory sync failure")
		original := journal.fs.syncDirectory
		writes := 0
		journal.fs.syncDirectory = func(path string) error {
			if path == journal.root {
				writes++
				if writes == 2 { // Begin reached disk; completion durability is uncertain.
					return want
				}
			}
			return original(path)
		}
		if err := journal.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); !errors.Is(err, want) {
			t.Fatalf("ambiguous completion was reported as committed: %v", err)
		}
		if err := journal.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); publicCode(err) != CodeRecoveryRequired {
			t.Fatalf("ambiguous completion was allowed to replay: %v", err)
		}
	})
}
