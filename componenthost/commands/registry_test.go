package commands

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func testCommand(name string) Command {
	return Command{
		Name:      name,
		UsageLine: name,
		Run: func([]string, io.Reader, io.Writer, io.Writer, func(string) string) int {
			return 0
		},
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	t.Cleanup(Register(testCommand("one")))

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate command registration did not panic")
		}
	}()
	Register(testCommand("one"))
}

func TestStaleCleanupDoesNotRemoveNewRegistration(t *testing.T) {
	cleanupOld := Register(testCommand("one"))
	cleanupOld()
	t.Cleanup(Register(testCommand("one")))

	cleanupOld()
	if _, ok := Run("one", nil, nil, io.Discard, io.Discard, func(string) string { return "" }); !ok {
		t.Fatal("stale cleanup removed a newer registration")
	}
}

func TestRegisterBoundsCardinality(t *testing.T) {
	for index := 0; index < maxRegisteredCommands; index++ {
		t.Cleanup(Register(testCommand(fmt.Sprintf("command-%03d", index))))
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registry accepted a command past its cardinality bound")
		}
	}()
	Register(testCommand("overflow"))
}

func TestRunContainsOptionalCommandPanic(t *testing.T) {
	t.Cleanup(Register(Command{Name: "panic", UsageLine: "panic", Run: func([]string, io.Reader, io.Writer, io.Writer, func(string) string) int {
		panic("secret")
	}}))
	var stderr strings.Builder
	code, ok := Run("panic", nil, nil, io.Discard, &stderr, func(string) string { return "" })
	if !ok || code == 0 || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("panicking command was not contained: code=%d stderr=%q", code, stderr.String())
	}
}
