package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func backendAddressSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func TestBackendAddressPublicationSharingViolation(t *testing.T) {
	const address = "127.0.0.1:12345\n"
	if path := os.Getenv("SUI_E2E_ADDRESS_LOCK_CHILD"); path != "" {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		// Publish and hold the file in the child, as in the composition fixture.
		handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		var written uint32
		if err := windows.WriteFile(handle, []byte(address), &written, nil); err != nil {
			t.Fatal(err)
		}
		if int(written) != len(address) {
			t.Fatal("incomplete address publication")
		}
		fmt.Fprintln(os.Stdout, "locked")
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		return
	}

	path := filepath.Join(t.TempDir(), "address.txt")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestBackendAddressPublicationSharingViolation$")
	command.Env = append(os.Environ(), "SUI_E2E_ADDRESS_LOCK_CHILD="+path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = stdin.Close()
		cancel()
		if !waited {
			_ = command.Wait()
		}
	}()
	if ready, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || ready != "locked\n" {
		t.Fatalf("address publisher did not hold its file: ready=%q err=%v", ready, err)
	}
	_, err = os.ReadFile(path)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("child's exclusive handle did not produce the observed sharing violation: %v", err)
	}
	if !backendAddressReadPending(err) {
		t.Fatalf("address publication sharing violation was treated as a terminal error: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	if err != nil {
		t.Fatalf("address publisher failed: %v\n%s", err, stderr.String())
	}
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
