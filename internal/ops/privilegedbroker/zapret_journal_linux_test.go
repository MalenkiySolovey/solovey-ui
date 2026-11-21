//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"io"
	"testing"
	"time"

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
		SocketMark: plan.SocketMark(), QueueMark: plan.QueueMark(), LoopMark: plan.LoopMark(), FinalRouteBit: plan.FinalRouteBit(),
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

	for _, fault := range []string{"short-write", "directory-sync"} {
		t.Run(fault, func(t *testing.T) {
			root := testJournalRoot(t)
			journal, err := openTestJournal(root, "boot-one")
			if err != nil {
				t.Fatal(err)
			}
			want := error(io.ErrShortWrite)
			if fault == "short-write" {
				journal.fs = journalFilesystemWithFileFault(journal.fs, journal.path, fault)
			} else {
				want = errors.New("injected directory sync failure")
				journal.fs.syncDirectory = func(string) error { return want }
			}
			if err := journal.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); !errors.Is(err, want) {
				t.Fatalf("failed commit was acknowledged: %v", err)
			}
			reopened, err := openTestJournal(root, "boot-one")
			if err != nil {
				t.Fatal(err)
			}
			if fault == "directory-sync" {
				// A complete row can be visible despite the sync error. Do
				// not issue a replacement grant from that ambiguous result.
				if err := reopened.recordZapretPath(t.Context(), checkpoint, grant.PeerRevision); publicCode(err) != CodeRecoveryRequired {
					t.Fatalf("ambiguously recorded generation did not fence replacement: %v", err)
				}
			}
			now := time.Now().UTC()
			foreign := brokerMutationRequest(t, now, 1, "unrelated-broker-mutation")
			digest := Digest(append(canonicalRequestAuthority(foreign), foreign.Payload...))
			if _, active, err := reopened.Begin(foreign, PeerIdentity{Revision: grant.PeerRevision}, digest, now); err != nil || active == nil {
				t.Fatalf("Zapret write failure fenced an unrelated broker mutation: %v", err)
			}
		})
	}

	t.Run("only verified kernel boot change allows replacement", func(t *testing.T) {
		boot, err := currentBootID()
		if err != nil {
			t.Fatal(err)
		}
		// A fixture receipt from a different boot; this test never reboots
		// the runner, detaches kernel resources, or opens an admission path.
		const previousBoot = "00000000-0000-4000-8000-000000000001"
		const claimedBoot = "00000000-0000-4000-8000-000000000002"
		root := testJournalRoot(t)
		previous, err := openTestJournal(root, previousBoot)
		if err != nil {
			t.Fatal(err)
		}
		prior := checkpoint
		prior.Generation = grant.Generation
		if err := previous.recordZapretPath(t.Context(), prior, grant.PeerRevision); err != nil {
			t.Fatal(err)
		}
		claimed, err := openTestJournal(root, claimedBoot)
		if err != nil {
			t.Fatal(err)
		}
		if err := claimed.recordZapretPath(t.Context(), checkpoint, newPeer.PeerRevision); publicCode(err) != CodeRecoveryRequired {
			t.Fatalf("claimed boot change bypassed recovery: %v", err)
		}
		current, err := openTestJournal(root, boot)
		if err != nil {
			t.Fatal(err)
		}
		if err := current.recordZapretPath(t.Context(), checkpoint, newPeer.PeerRevision); err != nil {
			t.Fatalf("verified new boot could not replace stale checkpoint: %v", err)
		}
		current, err = openTestJournal(root, boot)
		if err != nil {
			t.Fatal(err)
		}
		newGrant := newGeneration
		newGrant.PeerRevision = newPeer.PeerRevision
		if current.checkZapretPath(grant) == nil || current.checkZapretPath(newGrant) != nil {
			t.Fatal("reopened journal did not retain only the new authority")
		}
		different := checkpoint
		different.Generation = Digest([]byte("replacement-in-same-boot"))
		if err := current.recordZapretPath(t.Context(), different, grant.PeerRevision); publicCode(err) != CodeRecoveryRequired {
			t.Fatalf("same-boot broker restart or peer change bypassed recovery: %v", err)
		}
	})
}
