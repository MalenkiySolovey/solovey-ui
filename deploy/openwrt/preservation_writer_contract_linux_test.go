//go:build linux

package openwrt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	preservationWriterChildEnv = "SUI_PRESERVATION_WRITER_CHILD"
	preservationWriterRootEnv  = "SUI_PRESERVATION_WRITER_ROOT"
	changedServiceID           = 27111
)

// TestExtractedPreservationWriterContractWithChangedServiceIdentity starts
// with an archive made by the exact locked sysupgrade path, extracts it into a
// fresh root, changes the package account's numeric IDs, and runs the actual
// package preparation helper before the service account advances metadata.
func TestExtractedPreservationWriterContractWithChangedServiceIdentity(t *testing.T) {
	if os.Getenv(preservationWriterChildEnv) == "1" {
		runPreservationWriterChild(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("post-extraction ownership acceptance requires root for chroot and UID/GID switching")
	}

	source := newPreservationFixture(t)
	snapshot, err := os.ReadFile(preservationSnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata := source.metadata
	metadata.ArtifactPath = DefaultPreservationSnapshot
	metadata.Identity = preservationIdentity(metadata)
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes = append(metadataBytes, '\n')

	archiveRoot := newPreservationSysupgradeRoot(t)
	archiveRoot.writeMode(t, DefaultPreservationSnapshot, snapshot, 0o600)
	archiveRoot.writeMode(t, DefaultPreservationMetadata, metadataBytes, 0o600)
	archiveRoot.write(t, "/usr/lib/solovey-ui/solovey-openwrt-preservation", `#!/bin/sh
set -eu
[ "$1" = prepare ]
[ -s /etc/solovey-ui/db/sysupgrade-preservation/database.db ]
[ -s /etc/solovey-ui/db/sysupgrade-preservation/metadata.json ]
printf 'preservation-identity-diagnostic\n'
`)
	archiveRoot.sysupgrade(t, []string{"-b", "/tmp/preservation-writer.tgz"}, true)
	archive := archiveRoot.read(t, "/tmp/preservation-writer.tgz")

	pinned := filepath.Clean(filepath.Join("..", "..", "..", "..", "upstreams", "openwrt-openwrt-25.12.5"))
	pinned, err = filepath.Abs(pinned)
	if err != nil {
		t.Fatal(err)
	}
	assertPinnedOpenWrtRevision(t, pinned)
	fresh := newPinnedLifecycleRoot(t, pinned)
	extractPreservationArchive(t, archive, fresh.root)
	prepare, err := os.ReadFile("solovey-openwrt-prepare")
	if err != nil {
		t.Fatal(err)
	}
	fresh.writeMode(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", prepare, 0o755)
	fresh.writeMode(t, "/etc/passwd", []byte("root:x:0:0:root:/root:/bin/ash\nsolovey-ui:x:27111:27111:Solovey UI:/etc/solovey-ui:/sbin/nologin\n"), 0o644)
	fresh.writeMode(t, "/etc/group", []byte("root:x:0:\nsolovey-ui:x:27111:\n"), 0o644)
	for _, name := range []string{"solovey-openwrt-durability", "solovey-openwrt-owner-manifest", "solovey-openwrt-broker-manifest"} {
		fresh.write(t, "/usr/lib/solovey-ui/"+name, "#!/bin/sh\nexit 0\n")
	}
	if err := os.MkdirAll(fresh.hostPath("/run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fresh.hostPath("/tmp"), 0o1777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fresh.hostPath("/tmp"), 0o1777); err != nil {
		t.Fatal(err)
	}
	fresh.run(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", nil, true)
	identity, err := os.ReadFile(fresh.hostPath(DefaultOpenWrtInstanceIDPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(identity) != preservationKnownInstanceID+"\n" {
		t.Fatalf("package preparation changed extracted stable instance UUID: %q", identity)
	}

	for _, item := range []struct {
		name string
		mode os.FileMode
		max  int64
	}{
		{DefaultPreservationRoot, 0o700, 0},
		{DefaultPreservationSnapshot, 0o600, 512 << 20},
		{DefaultPreservationMetadata, 0o600, MaxPreservationMetadata},
	} {
		info, err := os.Lstat(fresh.hostPath(item.name))
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("repaired preservation member %s has no Linux ownership", item.name)
		}
		if stat.Uid != changedServiceID || stat.Gid != changedServiceID || info.Mode().Perm() != item.mode {
			t.Fatalf("repaired preservation member %s = mode %o owner %d:%d", item.name, info.Mode().Perm(), stat.Uid, stat.Gid)
		}
		if item.max > 0 && (!info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > item.max) {
			t.Fatalf("repaired preservation member %s is not a bounded regular file: %#v", item.name, info)
		}
	}

	command := exec.Command(os.Args[0], "-test.run=^TestExtractedPreservationWriterContractWithChangedServiceIdentity$", "-test.v")
	command.Env = append(os.Environ(), preservationWriterChildEnv+"=1", preservationWriterRootEnv+"="+fresh.root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("service-account preservation writer: %v\n%s", err, output)
	}
	data, err := os.ReadFile(fresh.hostPath(DefaultPreservationMetadata))
	if err != nil {
		t.Fatal(err)
	}
	terminal := PreservationMetadataV1{}
	if err := json.Unmarshal(data, &terminal); err != nil || terminal.State != PreservationStateRestored || terminal.RestoreOperationRev != 1 {
		t.Fatalf("service-account terminal metadata = %#v, %v", terminal, err)
	}
	if _, err := os.Lstat(fresh.hostPath(DefaultPreservationMetadata + ".partial")); !os.IsNotExist(err) {
		t.Fatalf("atomic partial metadata was not retired: %v", err)
	}
}

func TestTerminalTombstoneReclaimsModeledMaximumSparseSnapshot(t *testing.T) {
	newPreservationFixture(t)
	environment := testPreservationEnvironment()
	if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(preservationSnapshotPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	truncateErr := file.Truncate(512 << 20)
	closeErr := file.Close()
	if err := errors.Join(truncateErr, closeErr); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(preservationSnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 512<<20 {
		t.Fatalf("modeled maximum sparse snapshot size=%d error=%v", info.Size(), err)
	}
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), environment); err != nil {
		t.Fatalf("terminal maximum-snapshot cleanup: %v", err)
	}
	if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
		t.Fatalf("terminal maximum sparse snapshot remains: %v", err)
	}
}

func runPreservationWriterChild(t *testing.T) {
	root := os.Getenv(preservationWriterRootEnv)
	if root == "" {
		t.Fatal("preservation writer child root is missing")
	}
	if err := syscall.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setgroups([]int{}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setgid(changedServiceID); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setuid(changedServiceID); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	syncDirectory := func(name string) error {
		directory, err := os.Open(name)
		if err != nil {
			return err
		}
		return errors.Join(directory.Sync(), directory.Close())
	}
	metadata.State = PreservationStateRestoring
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, syncDirectory); err != nil {
		t.Fatal(err)
	}
	metadata.State = PreservationStateRestored
	metadata.RestoredAt = metadata.PreparedAt + 1
	metadata.RestoreOperationID = "data-operation:preservation-service-writer"
	metadata.RestoreOperationRev = 1
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, syncDirectory); err != nil {
		t.Fatal(err)
	}
	verified, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || verified.State != PreservationStateRestored {
		t.Fatalf("terminal preservation state = %#v, %v", verified, err)
	}
}
