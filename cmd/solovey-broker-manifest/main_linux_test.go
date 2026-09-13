//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestManifestPublicationOrdersDurabilityBeforeSuccess(t *testing.T) {
	directory := manifestPublicationDirectory(t)
	path := filepath.Join(directory, "broker-clients.json")
	data := manifestPublicationData(t, "new")
	events := []string{}
	fs := recordingManifestFilesystem(&events, "")
	if err := installManifest(path, data, fs, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "chmod", "chown", "write", "file-sync", "close", "rename", "directory-sync"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("publication order=%v, want=%v", events, want)
	}
	loaded, err := broker.LoadManifest(path)
	if err != nil || loaded.Clients[0].Name != "new" {
		t.Fatalf("published manifest=%#v err=%v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("published mode=%v err=%v", info, err)
	}
}

func TestManifestPublicationFaultsLeaveExactlyOneCompleteGeneration(t *testing.T) {
	for _, fault := range []string{"create", "chmod", "chown", "write", "file-sync", "close", "rename", "directory-sync"} {
		t.Run(fault, func(t *testing.T) {
			directory := manifestPublicationDirectory(t)
			path := filepath.Join(directory, "broker-clients.json")
			if err := os.WriteFile(path, manifestPublicationData(t, "old"), 0o640); err != nil {
				t.Fatal(err)
			}
			fs := recordingManifestFilesystem(nil, fault)
			err := installManifest(path, manifestPublicationData(t, "new"), fs, true)
			if err == nil {
				t.Fatalf("%s fault was acknowledged", fault)
			}
			loaded, loadErr := broker.LoadManifest(path)
			if loadErr != nil || len(loaded.Clients) != 1 {
				t.Fatalf("%s left no complete consumer generation: manifest=%#v err=%v", fault, loaded, loadErr)
			}
			want := "old"
			if fault == "directory-sync" {
				want = "new"
			}
			if loaded.Clients[0].Name != want {
				t.Fatalf("%s visible generation=%q, want=%q", fault, loaded.Clients[0].Name, want)
			}
		})
	}
}

func TestManifestPublicationRejectsOversizeBeforeMutation(t *testing.T) {
	directory := manifestPublicationDirectory(t)
	path := filepath.Join(directory, "broker-clients.json")
	events := []string{}
	if err := installManifest(path, []byte(strings.Repeat("x", (256<<10)+1)), recordingManifestFilesystem(&events, ""), true); err == nil {
		t.Fatal("oversized manifest was accepted")
	}
	if len(events) != 0 {
		t.Fatalf("oversized manifest mutated filesystem: %v", events)
	}
}

func TestSystemdManifestWriterUsesCoherentReleaseObjectTuple(t *testing.T) {
	path := manifestWriterExecutableFixture(t)
	entry, err := client("panel", path, 1001, 1001, false, broker.RolePanel)
	if err != nil {
		t.Fatal(err)
	}
	object, err := executableobject.Open(path, executableobject.Policy{MaxBytes: 512 << 20, AllowSymlink: true,
		RequireRegular: true, RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	identity := object.Identity()
	if entry.ExecutableDigest != identity.Digest || entry.Device != identity.Device || entry.Inode != identity.Inode ||
		entry.CgroupPolicy != broker.CgroupRequired || entry.CgroupAuthorityRevision != broker.CgroupAuthorityRevisionV1 {
		t.Fatalf("entry=%+v identity=%+v", entry, identity)
	}
}

func manifestWriterExecutableFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned release object is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-systemd-manifest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "solovey-ui")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

type recordingManifestFile struct {
	manifestFile
	events *[]string
	fault  string
	closed bool
}

func (f *recordingManifestFile) record(event string) error {
	if f.events != nil {
		*f.events = append(*f.events, event)
	}
	if f.fault == event {
		return errors.New("injected " + event + " failure")
	}
	return nil
}

func (f *recordingManifestFile) Chmod(mode os.FileMode) error {
	if err := f.record("chmod"); err != nil {
		return err
	}
	return f.manifestFile.Chmod(mode)
}

func (f *recordingManifestFile) Chown(uid, gid int) error {
	if err := f.record("chown"); err != nil {
		return err
	}
	return f.manifestFile.Chown(uid, gid)
}

func (f *recordingManifestFile) Write(data []byte) (int, error) {
	if err := f.record("write"); err != nil {
		if len(data) == 0 {
			return 0, err
		}
		written, _ := f.manifestFile.Write(data[:len(data)/2])
		return written, err
	}
	return f.manifestFile.Write(data)
}

func (f *recordingManifestFile) Sync() error {
	if err := f.record("file-sync"); err != nil {
		return err
	}
	return f.manifestFile.Sync()
}

func (f *recordingManifestFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	err := f.record("close")
	closeErr := f.manifestFile.Close()
	return errors.Join(err, closeErr)
}

func recordingManifestFilesystem(events *[]string, fault string) manifestFilesystem {
	fs := realManifestFilesystem()
	create := fs.createTemp
	fs.createTemp = func(directory, pattern string) (manifestFile, error) {
		if events != nil {
			*events = append(*events, "create")
		}
		if fault == "create" {
			return nil, errors.New("injected create failure")
		}
		file, err := create(directory, pattern)
		if err != nil {
			return nil, err
		}
		return &recordingManifestFile{manifestFile: file, events: events, fault: fault}, nil
	}
	rename := fs.rename
	fs.rename = func(oldPath, newPath string) error {
		if events != nil {
			*events = append(*events, "rename")
		}
		if fault == "rename" {
			return errors.New("injected rename failure")
		}
		return rename(oldPath, newPath)
	}
	syncDirectory := fs.syncDirectory
	fs.syncDirectory = func(path string) error {
		if events != nil {
			*events = append(*events, "directory-sync")
		}
		if fault == "directory-sync" {
			return errors.New("injected directory sync failure")
		}
		return syncDirectory(path)
	}
	return fs
}

func manifestPublicationDirectory(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned manifest publication fixture is required")
	}
	directory, err := os.MkdirTemp("/root", "solovey-systemd-manifest-publication-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	return directory
}

func manifestPublicationData(t *testing.T, name string) []byte {
	t.Helper()
	entry := broker.ClientManifest{Name: name, UID: 1001, GID: 1001, Executable: "/usr/local/solovey-ui/releases/current/solovey-ui",
		ExecutableDigest: strings.Repeat("a", 64), Device: 1, Inode: 2, Roles: []broker.Role{broker.RolePanel},
		CgroupUnit: "solovey-ui.service", CgroupPolicy: broker.CgroupRequired, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{entry}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
