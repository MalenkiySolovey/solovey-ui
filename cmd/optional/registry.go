package optional

import (
	"fmt"
	"io"
	"sort"
	"sync"
)

type Runner func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, getenv func(string) string) int

type Command struct {
	Name      string
	UsageLine string
	Run       Runner
}

var commands = struct {
	sync.RWMutex
	entries map[string]Command
}{
	entries: map[string]Command{},
}

func Register(command Command) func() {
	if command.Name == "" {
		panic("optional command name is required")
	}
	if command.UsageLine == "" {
		panic(fmt.Errorf("optional command %q usage line is required", command.Name))
	}
	if command.Run == nil {
		panic(fmt.Errorf("optional command %q runner is nil", command.Name))
	}

	commands.Lock()
	if _, exists := commands.entries[command.Name]; exists {
		commands.Unlock()
		panic(fmt.Errorf("optional command %q already registered", command.Name))
	}
	commands.entries[command.Name] = command
	commands.Unlock()

	return func() {
		commands.Lock()
		delete(commands.entries, command.Name)
		commands.Unlock()
	}
}

func UsageLines() []string {
	snapshot := commandSnapshot()
	lines := make([]string, 0, len(snapshot))
	for _, command := range snapshot {
		lines = append(lines, command.UsageLine)
	}
	return lines
}

func Run(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, getenv func(string) string) (int, bool) {
	commands.RLock()
	command, ok := commands.entries[name]
	commands.RUnlock()
	if !ok {
		return 0, false
	}
	return command.Run(args, stdin, stdout, stderr, getenv), true
}

func ResetForTest() {
	commands.Lock()
	commands.entries = map[string]Command{}
	commands.Unlock()
}

func commandSnapshot() []Command {
	commands.RLock()
	snapshot := make([]Command, 0, len(commands.entries))
	for _, command := range commands.entries {
		snapshot = append(snapshot, command)
	}
	commands.RUnlock()

	sort.Slice(snapshot, func(i, j int) bool {
		return snapshot[i].Name < snapshot[j].Name
	})
	return snapshot
}
