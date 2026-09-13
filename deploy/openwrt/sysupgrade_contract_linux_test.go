//go:build linux

package openwrt

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

const preservationKnownInstanceID = "00112233-4455-4677-8899-aabbccddeeff"

// TestPinnedSysupgradeIdentityStreamAndInventoryContract executes the exact
// locked OpenWrt 25.12.5 sysupgrade entrypoint and common/tar helpers in a
// root-owned chroot. Only the external Solovey preservation binary and host
// BusyBox applets whose build-time feature set differs are fixture adapters.
func TestPinnedSysupgradeIdentityStreamAndInventoryContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("exact pinned sysupgrade contract requires root for an isolated chroot")
	}
	fixture := newPreservationSysupgradeRoot(t)

	listStdout, listStderr := fixture.sysupgrade(t, []string{"-l"}, true)
	listed := nonEmptyLines(string(listStdout))
	want := []string{DefaultOpenWrtInstanceIDPath, DefaultPreservationSnapshot, DefaultPreservationMetadata}
	slices.Sort(listed)
	slices.Sort(want)
	if !slices.Equal(listed, want) {
		t.Fatalf("native sysupgrade -l members=%q want=%q stderr=%s", listed, want, listStderr)
	}
	for _, name := range []string{DefaultPreservationSnapshot, DefaultPreservationMetadata} {
		if _, err := os.Lstat(fixture.hostPath(name)); !os.IsNotExist(err) {
			t.Fatalf("native list mode mutated preservation member %s: %v", name, err)
		}
	}

	_, fileStderr := fixture.sysupgrade(t, []string{"-b", "/tmp/solovey-file.tgz"}, true)
	archive := fixture.read(t, "/tmp/solovey-file.tgz")
	if !slices.Equal(sortedPreservationMembers(t, archive), want) {
		t.Fatalf("native file backup members=%q want=%q stderr=%s", sortedPreservationMembers(t, archive), want, fileStderr)
	}
	if !bytes.Equal(fixture.read(t, DefaultOpenWrtInstanceIDPath), []byte(preservationKnownInstanceID+"\n")) {
		t.Fatal("native backup preparation changed the stable instance UUID")
	}
	if _, err := os.Lstat(fixture.hostPath(DefaultPreservationSnapshot)); !os.IsNotExist(err) {
		t.Fatalf("file backup retained terminal bulk snapshot: %v", err)
	}

	stream, streamStderr := fixture.sysupgrade(t, []string{"-b", "-"}, true)
	if len(stream) < 2 || stream[0] != 0x1f || stream[1] != 0x8b {
		t.Fatalf("stdout backup does not begin with gzip magic: %x stderr=%s", stream[:min(16, len(stream))], streamStderr)
	}
	streamPath := filepath.Join(t.TempDir(), "sysupgrade-stdout.tgz")
	if err := os.WriteFile(streamPath, stream, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("gzip", "-t", streamPath).CombinedOutput(); err != nil {
		t.Fatalf("gzip -t rejected native stdout backup: %v\n%s", err, output)
	}
	if !strings.Contains(string(streamStderr), "preservation-identity-diagnostic") {
		t.Fatalf("successful preservation diagnostic was not routed to stderr: %s", streamStderr)
	}
	if !slices.Equal(sortedPreservationMembers(t, stream), want) {
		t.Fatalf("stdout backup members=%q want=%q", sortedPreservationMembers(t, stream), want)
	}
	if _, err := os.Lstat(fixture.hostPath(DefaultPreservationSnapshot)); !os.IsNotExist(err) {
		t.Fatalf("stdout backup retained terminal bulk snapshot: %v", err)
	}

	fixture.sysupgrade(t, []string{"-q", "-b", "/tmp/solovey-quiet.tgz"}, true)
	if !slices.Equal(sortedPreservationMembers(t, fixture.read(t, "/tmp/solovey-quiet.tgz")), want) {
		t.Fatal("quiet file-output mode changed the Solovey backup inventory")
	}
	if events := string(fixture.read(t, "/tmp/preservation-events")); strings.Count(events, "backup-completed\n") != 3 {
		t.Fatalf("native backup terminalization count=%q", events)
	}

	fresh := t.TempDir()
	extractPreservationArchive(t, archive, fresh)
	identity := filepath.Join(fresh, filepath.FromSlash(strings.TrimPrefix(DefaultOpenWrtInstanceIDPath, "/")))
	if data, err := os.ReadFile(identity); err != nil || !bytes.Equal(data, []byte(preservationKnownInstanceID+"\n")) {
		t.Fatalf("fresh-root extracted instance identity=%q error=%v", data, err)
	}
}

func TestPinnedSysupgradeNativeRestoreRejectsBeforeSoloveyApplication(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("exact pinned sysupgrade restore rejection requires root for an isolated chroot")
	}
	fixture := newPreservationSysupgradeRoot(t)
	fixture.sysupgrade(t, []string{"-b", "/tmp/database-a.tgz"}, true)

	want := map[string][]byte{
		DefaultDatabaseFolder + "/solovey-ui.db": []byte("distinguishable-live-database-b\n"),
		DefaultPreservationSnapshot:              []byte("distinguishable-preservation-b\n"),
		DefaultPreservationMetadata:              []byte("distinguishable-metadata-b\n"),
	}
	for name, data := range want {
		fixture.writeMode(t, name, data, 0o600)
	}
	stdout, stderr := fixture.sysupgrade(t, []string{"-r", "/tmp/database-a.tgz"}, false)
	if len(stdout) != 0 || !strings.Contains(string(stderr), "does not support native sysupgrade backup restore; no files were applied") {
		t.Fatalf("native restore rejection channels: stdout=%q stderr=%q", stdout, stderr)
	}
	for name, expected := range want {
		if actual := fixture.read(t, name); !bytes.Equal(actual, expected) {
			t.Fatalf("native restore partially changed %s: got=%q want=%q", name, actual, expected)
		}
	}
}

func TestPinnedSysupgradeFailedBackupRetiresUnarchivedBulkGeneration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("exact pinned sysupgrade backup failure requires root for an isolated chroot")
	}
	fixture := newPreservationSysupgradeRoot(t)
	fixture.sysupgrade(t, []string{"-b", "/tmp/failed.tgz"}, false, "SUI_FAIL_TAR=1")
	if _, err := os.Lstat(fixture.hostPath("/tmp/failed.tgz")); !os.IsNotExist(err) {
		t.Fatalf("failed native backup retained an archive: %v", err)
	}
	if _, err := os.Lstat(fixture.hostPath(DefaultPreservationSnapshot)); !os.IsNotExist(err) {
		t.Fatalf("failed native backup retained unconsumed bulk snapshot: %v", err)
	}
	if metadata := string(fixture.read(t, DefaultPreservationMetadata)); metadata != "backup-failed\n" {
		t.Fatalf("failed native backup lacks terminal tombstone: %q", metadata)
	}
}

type preservationSysupgradeRoot struct {
	*pinnedLifecycleRoot
}

func newPreservationSysupgradeRoot(t *testing.T) *preservationSysupgradeRoot {
	t.Helper()
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	pinned := filepath.Join(workspaceRoot, "upstreams", "openwrt-openwrt-25.12.5")
	var err error
	pinned, err = filepath.Abs(pinned)
	if err != nil {
		t.Fatal(err)
	}
	assertPinnedOpenWrtRevision(t, pinned)
	base := newPinnedLifecycleRoot(t, pinned)
	fixture := &preservationSysupgradeRoot{pinnedLifecycleRoot: base}

	copyPinned := func(source, target string, mode os.FileMode) {
		data, err := os.ReadFile(filepath.Join(pinned, filepath.FromSlash(source)))
		if err != nil {
			t.Fatal(err)
		}
		fixture.writeMode(t, target, data, mode)
	}
	copyPinned("package/base-files/files/sbin/sysupgrade", "/sbin/sysupgrade", 0o755)
	copyPinned("package/base-files/files/lib/functions/system.sh", "/lib/functions/system.sh", 0o755)
	copyPinned("package/base-files/files/lib/upgrade/common.sh", "/lib/upgrade/common.sh", 0o755)
	copyPinned("package/base-files/files/lib/upgrade/tar.sh", "/lib/upgrade/tar.sh", 0o755)
	hook, err := os.ReadFile("solovey-ui-upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	fixture.writeMode(t, "/lib/upgrade/solovey-ui.sh", hook, 0o755)
	keep, err := os.ReadFile("solovey-ui.keep")
	if err != nil {
		t.Fatal(err)
	}
	fixture.writeMode(t, "/lib/upgrade/keep.d/solovey-ui", keep, 0o644)
	fixture.write(t, "/usr/share/libubox/jshn.sh", "# exact sysupgrade paths exercised here do not consume JSON helpers\n")
	fixture.writeMode(t, DefaultOpenWrtInstanceIDPath, []byte(preservationKnownInstanceID+"\n"), 0o400)
	// User switching itself is outside this Preservation contract; map the package name
	// to root so BusyBox start-stop-daemon can exercise its exact argument path
	// without relying on host UID semantics inside the isolated fixture.
	fixture.writeMode(t, "/etc/passwd", []byte("root:x:0:0:root:/root:/bin/ash\nsolovey-ui:x:0:0:Solovey UI:/etc/solovey-ui:/sbin/nologin\n"), 0o644)
	fixture.writeMode(t, "/etc/group", []byte("root:x:0:\nsolovey-ui:x:0:\n"), 0o644)
	preservationRoot := fixture.hostPath(DefaultPreservationRoot)
	if err := os.MkdirAll(preservationRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.write(t, "/etc/init.d/sysupgrade-fixture", "#!/bin/sh\n[ \"$1\" = enabled ]\n")
	_ = os.Remove(fixture.hostPath("/etc/init.d/solovey-ui"))
	if err := os.MkdirAll(fixture.hostPath("/etc/rc.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.hostPath("/tmp"), 0o1777); err != nil {
		t.Fatal(err)
	}
	fixture.writeMode(t, "/dev/zero", make([]byte, 4096), 0o666)
	proc := fixture.hostPath("/proc")
	if err := os.MkdirAll(proc, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount("proc", proc, "proc", 0, ""); err != nil {
		t.Fatalf("mount isolated proc fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Unmount(proc, 0); err != nil {
			t.Errorf("unmount isolated proc fixture: %v", err)
		}
	})

	fixture.write(t, "/usr/lib/solovey-ui/solovey-openwrt-preservation", `#!/bin/sh
set -eu
case "$1" in
  prepare)
    printf 'logical-snapshot\n' > /etc/solovey-ui/db/sysupgrade-preservation/database.db
    printf 'bounded-metadata\n' > /etc/solovey-ui/db/sysupgrade-preservation/metadata.json
    printf 'preservation-identity-diagnostic\n'
    ;;
  complete-backup|fail-backup)
	if [ "$1" = complete-backup ]; then terminal=backup-completed; else terminal=backup-failed; fi
	printf '%s\n' "$terminal" >> /tmp/preservation-events
    rm -f /etc/solovey-ui/db/sysupgrade-preservation/database.db
	printf '%s\n' "$terminal" > /etc/solovey-ui/db/sysupgrade-preservation/metadata.json
    ;;
  *) exit 1 ;;
esac
`)
	fixture.write(t, "/bin/start-stop-daemon", `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  case "$1" in
    -x) helper="$2"; shift 2 ;;
    --) shift; break ;;
    *) shift ;;
  esac
done
exec "$helper" "$@"
`)
	fixture.write(t, "/bin/logger", "#!/bin/sh\nexit 0\n")
	// Ubuntu's static BusyBox omits tar -T while the locked OpenWrt BusyBox
	// enables it. Preserve sysupgrade's exact invocation and adapt that one
	// missing applet feature inside the chroot.
	if err := os.Remove(fixture.hostPath("/bin/tar")); err != nil {
		t.Fatal(err)
	}
	fixture.write(t, "/bin/tar", `#!/bin/sh
set -eu
[ "${SUI_FAIL_TAR:-0}" != 1 ] || exit 1
operation="$1"
shift
root=/
list=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -C) root="$2"; shift 2 ;;
    -T) list="$2"; shift 2 ;;
    *) exit 64 ;;
  esac
done
[ -n "$list" ]
cd "$root"
exec /bin/busybox tar "$operation" -f - $(cat "$list")
`)
	functionsPath := fixture.hostPath("/lib/functions.sh")
	functions, err := os.ReadFile(functionsPath)
	if err != nil {
		t.Fatal(err)
	}
	functions = append(functions, []byte("\n# fixture adapter for host BusyBox without CONFIG_FEATURE_TAR_FROM\ntar() { /bin/tar \"$@\"; }\n")...)
	if err := os.WriteFile(functionsPath, functions, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *preservationSysupgradeRoot) sysupgrade(t testing.TB, arguments []string, success bool, extraEnvironment ...string) ([]byte, []byte) {
	t.Helper()
	command := exec.Command("chroot", append([]string{f.root, "/sbin/sysupgrade"}, arguments...)...)
	command.Env = append([]string{"PATH=/bin:/sbin", "HOME=/", "LANG=C", "LC_ALL=C"}, extraEnvironment...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if success && err != nil {
		t.Fatalf("pinned sysupgrade %q failed: %v\nstdout=%x\nstderr=%s", arguments, err, stdout.Bytes(), stderr.String())
	}
	if !success && err == nil {
		t.Fatalf("pinned sysupgrade %q unexpectedly succeeded", arguments)
	}
	return stdout.Bytes(), stderr.Bytes()
}

func (f *preservationSysupgradeRoot) read(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(f.hostPath(name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sortedPreservationMembers(t testing.TB, archive []byte) []string {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	var result []string
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := "/" + strings.TrimPrefix(filepath.ToSlash(header.Name), "/")
		if name == DefaultOpenWrtInstanceIDPath || name == DefaultPreservationSnapshot || name == DefaultPreservationMetadata {
			result = append(result, name)
		}
	}
	slices.Sort(result)
	return result
}

func extractPreservationArchive(t testing.TB, archive []byte, root string) {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		name := "/" + strings.TrimPrefix(filepath.ToSlash(header.Name), "/")
		if name != DefaultOpenWrtInstanceIDPath && name != DefaultPreservationSnapshot && name != DefaultPreservationMetadata {
			continue
		}
		destination := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode).Perm())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(file, tarReader); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func nonEmptyLines(value string) []string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
