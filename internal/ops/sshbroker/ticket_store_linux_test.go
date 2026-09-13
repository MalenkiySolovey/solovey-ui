//go:build linux

package sshbroker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProofTicketStorePrunesTerminalStateBeforeCapacity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ssh-proof")
	store := newProofTicketStore(root, realProofTicketFilesystem(), false)
	issued := int64(1_900_000_000)
	for index := 0; index < maxProofTicketEntries+40; index++ {
		ticket := testProofTicket(fmt.Sprintf("expired-%03d", index), issued)
		switch index % 3 {
		case 1:
			ticket.ProofedAt = issued + 2
			ticket.ConsumedAt = issued + 3
			ticket.ExpiresAt = issued + 600
		case 2:
			ticket.ProofedAt = issued + 2
			ticket.ExpiresAt = issued + 30
		default:
			ticket.ExpiresAt = issued + 30
		}
		writeRawProofTicket(t, root, ticket)
	}
	for index := 0; index < 20; index++ {
		path := filepath.Join(root, fmt.Sprintf("%s%d", proofTicketTemporaryPrefix, index))
		if err := os.WriteFile(path, []byte("interrupted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	foreignTemp := filepath.Join(root, proofTicketTemporaryPrefix+"foreign")
	if err := os.WriteFile(foreignTemp, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	current := testProofTicket("current", issued)
	current.ExpiresAt = issued + 600
	writeRawProofTicket(t, root, current)

	store.now = func() time.Time { return time.Unix(issued+60, 0) }
	got, err := store.readAll()
	if err != nil {
		t.Fatalf("terminal entries blocked current ticket: %v", err)
	}
	if len(got) != 1 || got[0].OperationID != current.OperationID {
		t.Fatalf("read tickets=%#v, want only current live authority", got)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign temporary was removed or changed: %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 2 {
		t.Fatalf("post-prune directory entries=%d err=%v, want current plus foreign", len(entries), err)
	}
}

func TestProofTicketStoreNeverPrunesLiveAuthorityToSatisfyCapacity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ssh-proof")
	store := newProofTicketStore(root, realProofTicketFilesystem(), false)
	issued := int64(1_900_000_000)
	for index := 0; index < maxProofTicketEntries+1; index++ {
		ticket := testProofTicket(fmt.Sprintf("live-%03d", index), issued)
		ticket.ExpiresAt = issued + 600
		writeRawProofTicket(t, root, ticket)
	}
	store.now = func() time.Time { return time.Unix(issued+1, 0) }
	if _, err := store.readAll(); err == nil {
		t.Fatal("live-ticket overflow was silently pruned")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	jsonCount := 0
	for _, entry := range entries {
		if isProofTicketName(entry.Name()) {
			jsonCount++
		}
	}
	if jsonCount != maxProofTicketEntries+1 {
		t.Fatalf("live authority files=%d, want %d", jsonCount, maxProofTicketEntries+1)
	}
}

func TestProofTicketStoreWriterAndRestartStayWithinCapacity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ssh-proof")
	issued := int64(1_900_000_000)
	store := newProofTicketStore(root, realProofTicketFilesystem(), false)
	store.now = func() time.Time { return time.Unix(issued+1, 0) }
	for index := 0; index < maxProofTicketEntries; index++ {
		ticket := testProofTicket(fmt.Sprintf("writer-%03d", index), issued)
		ticket.ExpiresAt = issued + 600
		if err := store.write(ticket); err != nil {
			t.Fatalf("write %d: %v", index, err)
		}
	}
	overflow := testProofTicket("writer-overflow", issued)
	overflow.ExpiresAt = issued + 600
	if err := store.write(overflow); err == nil {
		t.Fatal("writer acknowledged a 129th live ticket")
	}
	restarted := newProofTicketStore(root, realProofTicketFilesystem(), false)
	restarted.now = store.now
	got, err := restarted.readAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxProofTicketEntries {
		t.Fatalf("restart returned %d live tickets, want %d", len(got), maxProofTicketEntries)
	}
}

func TestProofedTicketRemainsUntilConsumed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ssh-proof")
	issued := int64(1_900_000_000)
	store := newProofTicketStore(root, realProofTicketFilesystem(), false)
	store.now = func() time.Time { return time.Unix(issued+1, 0) }
	ticket := testProofTicket("proofed", issued)
	ticket.ExpiresAt = issued + 600
	if err := store.write(ticket); err != nil {
		t.Fatal(err)
	}
	ticket.ProofedAt = issued + 2
	if err := store.write(ticket); err != nil {
		t.Fatal(err)
	}
	if _, err := store.readOne(ticket.OperationID); err != nil {
		t.Fatalf("proofed ticket was pruned before verification: %v", err)
	}
	ticket.ConsumedAt = issued + 3
	if err := store.write(ticket); err != nil {
		t.Fatal(err)
	}
	if tickets, err := store.readAll(); err != nil || len(tickets) != 0 {
		t.Fatalf("consumed ticket remained readable after deterministic pruning: tickets=%#v err=%v", tickets, err)
	}
}

func TestProofTicketStoreFirstRootAndPublicationFaults(t *testing.T) {
	issued := int64(1_900_000_000)
	tests := []struct {
		name string
		edit func(*proofTicketFilesystem, string)
	}{
		{name: "root-create", edit: func(fs *proofTicketFilesystem, _ string) {
			fs.mkdir = func(string, os.FileMode) error { return errors.New("injected root create failure") }
		}},
		{name: "parent-sync", edit: func(fs *proofTicketFilesystem, root string) {
			fs.syncDirectory = func(path string) error {
				if path == filepath.Dir(root) {
					return errors.New("injected parent sync failure")
				}
				return syncDirectory(path)
			}
		}},
		{name: "file-sync", edit: func(fs *proofTicketFilesystem, _ string) {
			original := fs.createTemp
			fs.createTemp = func(directory, pattern string) (proofTicketFile, error) {
				file, err := original(directory, pattern)
				if err != nil {
					return nil, err
				}
				return &faultProofTicketFile{proofTicketFile: file, fault: "sync"}, nil
			}
		}},
		{name: "rename", edit: func(fs *proofTicketFilesystem, _ string) {
			fs.rename = func(string, string) error { return errors.New("injected rename failure") }
		}},
		{name: "child-sync", edit: func(fs *proofTicketFilesystem, root string) {
			fs.syncDirectory = func(path string) error {
				if path == root {
					return errors.New("injected child sync failure")
				}
				return syncDirectory(path)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ssh-proof")
			fs := realProofTicketFilesystem()
			test.edit(&fs, root)
			store := newProofTicketStore(root, fs, false)
			store.now = func() time.Time { return time.Unix(issued+1, 0) }
			ticket := testProofTicket("fault-"+test.name, issued)
			ticket.ExpiresAt = issued + 600
			if err := store.write(ticket); err == nil {
				t.Fatalf("%s fault was acknowledged", test.name)
			}
		})
	}

	t.Run("successful-write-reopens", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ssh-proof")
		store := newProofTicketStore(root, realProofTicketFilesystem(), false)
		store.now = func() time.Time { return time.Unix(issued+1, 0) }
		ticket := testProofTicket("durable", issued)
		ticket.ExpiresAt = issued + 600
		if err := store.write(ticket); err != nil {
			t.Fatal(err)
		}
		restarted := newProofTicketStore(root, realProofTicketFilesystem(), false)
		restarted.now = store.now
		got, err := restarted.readOne(ticket.OperationID)
		if err != nil || got.OperationID != ticket.OperationID {
			t.Fatalf("restart ticket=%#v err=%v", got, err)
		}
	})
}

func TestProofTicketStoreValidatesRootOwnershipOnProductionPath(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership validation requires the Linux root test user")
	}
	root := filepath.Join(t.TempDir(), "ssh-proof")
	store := newProofTicketStore(root, realProofTicketFilesystem(), true)
	store.now = func() time.Time { return time.Unix(1_900_000_001, 0) }
	ticket := testProofTicket("root-owned", 1_900_000_000)
	if err := store.write(ticket); err != nil {
		t.Fatal(err)
	}
	if _, err := newProofTicketStore(root, realProofTicketFilesystem(), true).readOne(ticket.OperationID); err != nil {
		t.Fatalf("root-owned ticket did not reopen: %v", err)
	}
}

type faultProofTicketFile struct {
	proofTicketFile
	fault string
}

func (f *faultProofTicketFile) Sync() error {
	if f.fault == "sync" {
		return errors.New("injected ticket file sync failure")
	}
	return f.proofTicketFile.Sync()
}

func testProofTicket(operationID string, issued int64) proofTicketV1 {
	digestValue := strings.Repeat("a", 64)
	return proofTicketV1{Schema: 2, OperationID: operationID, MarkerDigest: digestValue, Verifier: "verifier-" + operationID,
		EndpointID: "management:ssh:configured:ipv4:22", PrincipalID: "principal-" + operationID, AuthenticationClass: "publickey",
		BinaryRevision: digestValue, ServiceRevision: digestValue, Configuration: digestValue, IssuedAt: issued, IssuedAtMillis: issued * 1000, ExpiresAt: issued + 600}
}

func writeRawProofTicket(t *testing.T, root string, ticket proofTicketV1) {
	t.Helper()
	data, err := encodeProofTicket(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ticketPathFor(root, ticket.OperationID), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
