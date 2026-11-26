//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"io"
	"testing"

	"golang.org/x/sys/unix"
)

func TestZapretLuaBundleIsSealed(t *testing.T) {
	const program = `function observe() return VERDICT_PASS end`
	file, err := openZapretLuaBundle(program)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte("modified"), 0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("sealed bundle could be replaced: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil || string(data) != program {
		t.Fatalf("sealed Lua bundle bytes differ: %v", err)
	}
}
