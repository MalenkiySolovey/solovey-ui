package logging

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

type LogLevel string

const (
	Debug LogLevel = "debug"
	Info  LogLevel = "info"
	Warn  LogLevel = "warn"
	Error LogLevel = "error"
)

func GetLogLevel() LogLevel {
	if IsDebug() {
		return Debug
	}
	logLevel := strings.ToLower(strings.TrimSpace(os.Getenv("SUI_LOG_LEVEL")))
	if logLevel == "" {
		return Info
	}
	level := LogLevel(logLevel)
	if isValidLogLevel(level) {
		return level
	}
	fmt.Fprintf(os.Stderr, "WARNING - invalid SUI_LOG_LEVEL %q; falling back to %q\n", logLevel, Info)
	return Info
}

func isValidLogLevel(level LogLevel) bool {
	switch level {
	case Debug, Info, Warn, Error:
		return true
	default:
		return false
	}
}

func IsDebug() bool {
	return os.Getenv("SUI_DEBUG") == "true"
}

// IsSafeLogOutputPath reports whether a sing-box log.output value is safe to
// use as a file destination. Only stdout/stderr and relative paths that stay
// inside the panel directory are allowed.
func IsSafeLogOutputPath(output string) bool {
	switch output {
	case "", "stdout", "stderr":
		return true
	}
	normalized := strings.ReplaceAll(output, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return false
	}
	if len(normalized) >= 2 && normalized[1] == ':' && isASCIILetter(normalized[0]) {
		return false
	}
	return !slices.Contains(strings.Split(normalized, "/"), "..")
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
