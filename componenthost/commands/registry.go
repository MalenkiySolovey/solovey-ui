package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"
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
	if !validCommandName(command.Name) {
		panic("optional command name is required")
	}
	trimmedUsage := strings.TrimSpace(command.UsageLine)
	if trimmedUsage == "" || len(command.UsageLine) > 256 ||
		strings.ContainsAny(command.UsageLine, "\x00\r\n\t") ||
		(trimmedUsage != command.Name && !strings.HasPrefix(trimmedUsage, command.Name+" ")) {
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

	var once sync.Once
	return func() {
		once.Do(func() {
			commands.Lock()
			if current, ok := commands.entries[command.Name]; ok && current.token == token {
				delete(commands.entries, command.Name)
			}
			commands.Unlock()
		})
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
	return safeRun(entry.command.Run, args, stdin, stdout, stderr, getenv), true
}

func safeRun(runner Runner, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, getenv func(string) string) (code int) {
	defer func() {
		if recover() != nil {
			code = 1
			if stderr != nil {
				_, _ = io.WriteString(stderr, "optional command failed\n")
			}
		}
	}()
	return runner(args, stdin, stdout, stderr, getenv)
}

func validCommandName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
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
