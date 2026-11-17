package restorestate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverRollsBackEveryUncommittedSwapAndKeepsCommittedCandidate(t *testing.T) {
	for _, state := range []string{StateLiveMovePending, StateCandidatePending} {
		t.Run(state, func(t *testing.T) {
			directory := t.TempDir()
			live := filepath.Join(directory, "s-ui.db")
			candidate := []byte("candidate")
			if err := os.WriteFile(live, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(StagingPath(live), candidate, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Begin(live, digestBytes(candidate)); err != nil {
				t.Fatal(err)
			}
			if err := Transition(live, StateStaged, StateLiveMovePending); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(live, FallbackPath(live)); err != nil {
				t.Fatal(err)
			}
			if state == StateCandidatePending {
				if err := Transition(live, StateLiveMovePending, StateCandidatePending); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(StagingPath(live), live); err != nil {
					t.Fatal(err)
				}
			}
			if err := Recover(live); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(live)
			if err != nil || string(data) != "previous" {
				t.Fatalf("recovered live database=%q err=%v", data, err)
			}
			for _, path := range []string{StagingPath(live), FallbackPath(live), MarkerPath(live)} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("recovery left %s: %v", filepath.Base(path), err)
				}
			}
		})
	}

	t.Run("committed", func(t *testing.T) {
		directory := t.TempDir()
		live := filepath.Join(directory, "s-ui.db")
		candidate := []byte("candidate")
		if err := os.WriteFile(live, []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(StagingPath(live), candidate, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Begin(live, digestBytes(candidate)); err != nil {
			t.Fatal(err)
		}
		if err := Transition(live, StateStaged, StateLiveMovePending); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(live, FallbackPath(live)); err != nil {
			t.Fatal(err)
		}
		if err := Transition(live, StateLiveMovePending, StateCandidatePending); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(StagingPath(live), live); err != nil {
			t.Fatal(err)
		}
		if err := Transition(live, StateCandidatePending, StateCommitted); err != nil {
			t.Fatal(err)
		}
		if err := Recover(live); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(live)
		if err != nil || string(data) != "candidate" {
			t.Fatalf("committed live database=%q err=%v", data, err)
		}
	})
}

func TestRecoverRejectsMalformedOrOrphanedAuthority(t *testing.T) {
	directory := t.TempDir()
	live := filepath.Join(directory, "s-ui.db")
	if err := os.WriteFile(live, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FallbackPath(live), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Recover(live); err == nil {
		t.Fatal("orphaned fallback was silently adopted")
	}
	if err := os.Remove(FallbackPath(live)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MarkerPath(live), []byte(`{"schema":"solovey.restore-swap/v1","state":"RAW","candidateDigest":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Recover(live); err == nil {
		t.Fatal("malformed marker was silently adopted")
	}
}

func TestCommittedBoundaryKeepsFallbackUntilExplicitFinalization(t *testing.T) {
	directory := t.TempDir()
	live := filepath.Join(directory, "s-ui.db")
	candidate := []byte("candidate")
	if err := os.WriteFile(live, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StagingPath(live), candidate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Begin(live, digestBytes(candidate)); err != nil {
		t.Fatal(err)
	}
	if err := Transition(live, StateStaged, StateLiveMovePending); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(live, FallbackPath(live)); err != nil {
		t.Fatal(err)
	}
	if err := Transition(live, StateLiveMovePending, StateCandidatePending); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(StagingPath(live), live); err != nil {
		t.Fatal(err)
	}
	if err := MarkCommitted(live); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(FallbackPath(live)); err != nil {
		t.Fatalf("acceptance removed rollback authority before finalization: %v", err)
	}
	if err := FinalizeCommitted(live); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{FallbackPath(live), MarkerPath(live)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("finalization left %s: %v", filepath.Base(path), err)
		}
	}
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
