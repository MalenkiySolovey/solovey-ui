package commands

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

const maxRegisteredCommands = 128

type registeredCommand struct {
	command Command
	token   uint64
}

var commands = struct {
	sync.RWMutex
	entries   map[string]registeredCommand
	nextToken uint64
}{
	entries: map[string]registeredCommand{},
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
	if len(commands.entries) >= maxRegisteredCommands {
		commands.Unlock()
		panic("optional command registry capacity exceeded")
	}
	commands.nextToken++
	token := commands.nextToken
	commands.entries[command.Name] = registeredCommand{command: command, token: token}
	commands.Unlock()

	return func() {
		commands.Lock()
		if current, ok := commands.entries[command.Name]; ok && current.token == token {
			delete(commands.entries, command.Name)
		}
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
	entry, ok := commands.entries[name]
	commands.RUnlock()
	if !ok {
		return 0, false
	}
	return entry.command.Run(args, stdin, stdout, stderr, getenv), true
}

func ResetForTest() {
	commands.Lock()
	commands.entries = map[string]registeredCommand{}
	commands.Unlock()
}

func commandSnapshot() []Command {
	commands.RLock()
	snapshot := make([]Command, 0, len(commands.entries))
	for _, entry := range commands.entries {
		snapshot = append(snapshot, entry.command)
	}
	commands.RUnlock()

	sort.Slice(snapshot, func(i, j int) bool {
		return snapshot[i].Name < snapshot[j].Name
	})
	return snapshot
}
