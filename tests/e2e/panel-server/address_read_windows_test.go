package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func backendAddressSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func TestBackendAddressPublicationSharingViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "address.txt")
	const address = "127.0.0.1:12345\n"
	if err := os.WriteFile(path, []byte(address), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}
	}()
	_, err = os.ReadFile(path)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("exclusive handle did not produce the observed sharing violation: %v", err)
	}
	if !backendAddressReadPending(err) {
		t.Fatalf("address publication sharing violation was treated as a terminal error: %v", err)
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	handle = windows.InvalidHandle
	data, err := os.ReadFile(path)
	if err != nil || string(data) != address || backendAddressReadPending(err) {
		t.Fatalf("published address was not readable after unlock: data=%q err=%v", data, err)
	}
	for _, err := range []error{
		&os.PathError{Op: "open", Path: path, Err: windows.ERROR_ACCESS_DENIED},
		&os.PathError{Op: "open", Path: path, Err: windows.ERROR_INVALID_NAME},
		io.ErrUnexpectedEOF,
	} {
		if backendAddressReadPending(err) {
			t.Fatalf("permanent address error was treated as pending: %v", err)
		}
	}
}
