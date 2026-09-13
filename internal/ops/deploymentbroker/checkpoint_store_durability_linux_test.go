//go:build linux

package deploymentbroker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func TestCheckpointFirstCheckpointRootRequiresParentDurability(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "deployment")
	barriers := 0
	fault := errors.New("parent sync fault")
	if err := ensureRootDirectoryWithSync(root, 0o700, func(path string) error {
		barriers++
		if path != parent {
			t.Fatalf("parent barrier path=%q, want %q", path, parent)
		}
		return fault
	}); !errors.Is(err, fault) {
		t.Fatalf("first root parent-sync fault=%v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("fault model did not retain possible new root: %v", err)
	}
	if err := ensureRootDirectoryWithSync(root, 0o700, func(path string) error {
		barriers++
		return syncDir(path)
	}); err != nil {
		t.Fatal(err)
	}
	if barriers != 2 {
		t.Fatalf("parent barriers=%d", barriers)
	}
	checkpoint := checkpointCheckpoint("first-root", time.Now().UTC())
	if err := writeCheckpointAt(root, checkpoint); err != nil {
		t.Fatal(err)
	}
	reopened, err := readCheckpointAt(root, checkpoint.Revision)
	if err != nil || reopened.OperationID != checkpoint.OperationID {
		t.Fatalf("reopened=%#v err=%v", reopened, err)
	}
}

func TestCheckpointStoreReclaimsOwnedInterruptionsAndHistoricalReleases(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	root := filepath.Join(t.TempDir(), "deployment")
	if err := ensureRootDirectory(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxCheckpoints+32; index++ {
		temporary, err := os.CreateTemp(root, ".solovey-deployment-")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := temporary.Write([]byte("interrupted")); err != nil {
			t.Fatal(err)
		}
		if err := temporary.Close(); err != nil {
			t.Fatal(err)
		}
	}
	first := checkpointCheckpoint("after-temporaries", time.Now().UTC())
	if err := writeCheckpointAt(root, first); err != nil {
		t.Fatalf("owned temporaries exhausted admission: %v", err)
	}
	count, _, err := checkpointStoreUsage(root)
	if err != nil || count != 1 {
		t.Fatalf("checkpoint usage count=%d err=%v", count, err)
	}
	if err := removeCheckpointAt(root, first.Revision); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxCheckpoints+32; index++ {
		checkpoint := checkpointCheckpoint(fmt.Sprintf("historical-%03d", index), time.Now().UTC().Add(time.Duration(index)*time.Second))
		if err := writeCheckpointAt(root, checkpoint); err != nil {
			t.Fatalf("historical checkpoint %d: %v", index, err)
		}
		if err := removeCheckpointAt(root, checkpoint.Revision); err != nil {
			t.Fatalf("historical release %d: %v", index, err)
		}
	}
	count, bytes, err := checkpointStoreUsage(root)
	if err != nil || count != 0 || bytes != 0 {
		t.Fatalf("post-release usage count=%d bytes=%d err=%v", count, bytes, err)
	}
}

func TestCheckpointStorePreservesLiveAuthorityAtCapacity(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	root := filepath.Join(t.TempDir(), "deployment")
	checkpoints := make([]checkpointV1, 0, maxCheckpoints)
	for index := 0; index < maxCheckpoints; index++ {
		checkpoint := checkpointCheckpoint(fmt.Sprintf("live-%03d", index), time.Now().UTC().Add(time.Duration(index)*time.Second))
		if err := writeCheckpointAt(root, checkpoint); err != nil {
			t.Fatalf("live checkpoint %d: %v", index, err)
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	overflow := checkpointCheckpoint("overflow", time.Now().UTC().Add(time.Hour))
	if err := writeCheckpointAt(root, overflow); err == nil {
		t.Fatal("live checkpoint capacity was evicted or exceeded")
	}
	for _, checkpoint := range checkpoints {
		if _, err := readCheckpointAt(root, checkpoint.Revision); err != nil {
			t.Fatalf("live checkpoint %s was pruned: %v", checkpoint.OperationID, err)
		}
	}
	if err := removeCheckpointAt(root, checkpoints[0].Revision); err != nil {
		t.Fatal(err)
	}
	if err := writeCheckpointAt(root, overflow); err != nil {
		t.Fatalf("released capacity did not admit next checkpoint: %v", err)
	}
}

func checkpointCheckpoint(suffix string, now time.Time) checkpointV1 {
	checkpoint := checkpointV1{Schema: 1, OperationID: "deployment-operation:checkpoint-" + suffix,
		FromProfile: domain.NativeLegacyRoot, TargetProfile: domain.NativeHardened,
		ExpectedPosture: domain.Revision("posture-" + suffix), CreatedAt: now.Unix()}
	checkpoint.Revision = checkpointRevision(checkpoint)
	return checkpoint
}

func requireRootDeploymentBrokerTest(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("deployment checkpoint ownership tests require root")
	}
}
