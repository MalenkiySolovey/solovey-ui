package commands

import (
	"io"
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
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(testCommand("one"))

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate command registration did not panic")
		}
	}()
	Register(testCommand("one"))
}

func TestStaleCleanupDoesNotRemoveNewRegistration(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	cleanupOld := Register(testCommand("one"))
	ResetForTest()
	Register(testCommand("one"))

	cleanupOld()
	if _, ok := Run("one", nil, nil, io.Discard, io.Discard, func(string) string { return "" }); !ok {
		t.Fatal("stale cleanup removed a newer registration")
	}
}

func TestRegisterBoundsCardinality(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	for index := 0; index < maxRegisteredCommands; index++ {
		Register(testCommand(string(rune(index + 1))))
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registry accepted a command past its cardinality bound")
		}
	}()
	Register(testCommand("overflow"))
}
