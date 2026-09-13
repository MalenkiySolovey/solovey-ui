//go:build linux

package deploymentbroker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointPublicationFaultMatrixReopensOldOrNewGeneration(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "checkpoint.json")
	oldData, newData := []byte(`{"generation":"old"}`), []byte(`{"generation":"new"}`)
	fault := errors.New("checkpoint publication fault")
	for _, test := range []struct {
		name    string
		faultAt string
		want    []byte
	}{
		{name: "file-create", faultAt: "create", want: oldData},
		{name: "file-write", faultAt: "write", want: oldData},
		{name: "file-sync", faultAt: "sync", want: oldData},
		{name: "file-close", faultAt: "close", want: oldData},
		{name: "rename", faultAt: "rename", want: oldData},
		{name: "child-directory-sync", faultAt: "sync-dir", want: newData},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := atomicWrite(path, oldData, 0o600, 0, 0); err != nil {
				t.Fatal(err)
			}
			ops := productionDeploymentAtomicOps
			ops.createTemp = func(directory, pattern string) (deploymentAtomicFile, error) {
				if test.faultAt == "create" {
					return nil, fault
				}
				file, err := os.CreateTemp(directory, pattern)
				if err != nil {
					return nil, err
				}
				return &checkpointFaultAtomicFile{File: file, faultAt: test.faultAt, fault: fault}, nil
			}
			if test.faultAt == "rename" {
				ops.rename = func(string, string) error { return fault }
			}
			if test.faultAt == "sync-dir" {
				ops.syncDir = func(string) error { return fault }
			}
			if err := atomicWriteWithOps(ops, path, newData, 0o600, 0, 0); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			reopened, err := os.ReadFile(path)
			if err != nil || string(reopened) != string(test.want) {
				t.Fatalf("reopened=%q want=%q err=%v", reopened, test.want, err)
			}
		})
	}
}

type checkpointFaultAtomicFile struct {
	*os.File
	faultAt string
	fault   error
}

func (f *checkpointFaultAtomicFile) Write(data []byte) (int, error) {
	if f.faultAt == "write" {
		return 0, f.fault
	}
	return f.File.Write(data)
}

func (f *checkpointFaultAtomicFile) Sync() error {
	if f.faultAt == "sync" {
		return f.fault
	}
	return f.File.Sync()
}

func (f *checkpointFaultAtomicFile) Close() error {
	err := f.File.Close()
	if f.faultAt == "close" && err == nil {
		return f.fault
	}
	return err
}
