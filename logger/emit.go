package logger

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const MaxMessageBytes = 16 * 1024

// SanitizeMessage is the common final logger sink guard used by panel, slog,
// and observable core logging.
func SanitizeMessage(message string) string {
	return redact.StringLimit(message, MaxMessageBytes)
}

func Debug(args ...interface{}) { logPanel(slog.LevelDebug, fmt.Sprint(args...)) }
func Info(args ...interface{})  { logPanel(slog.LevelInfo, fmt.Sprint(args...)) }
func Infof(format string, args ...interface{}) {
	logPanel(slog.LevelInfo, fmt.Sprintf(format, args...))
}
func Warning(args ...interface{}) { logPanel(slog.LevelWarn, fmt.Sprint(args...)) }
func Warningf(format string, args ...interface{}) {
	logPanel(slog.LevelWarn, fmt.Sprintf(format, args...))
}
func Error(args ...interface{}) { logPanel(slog.LevelError, fmt.Sprint(args...)) }

func CoreDebug(args ...interface{})   { logCore("DEBUG", fmt.Sprint(args...)) }
func CoreInfo(args ...interface{})    { logCore("INFO", fmt.Sprint(args...)) }
func CoreWarning(args ...interface{}) { logCore("WARNING", fmt.Sprint(args...)) }
func CoreError(args ...interface{})   { logCore("ERROR", fmt.Sprint(args...)) }

func logPanel(level slog.Level, message string) {
	logWithSource("panel", level, message)
}

func logCore(level, message string) {
	logWithSource("core", parseSlogLevel(level), message)
}

func logWithSource(source string, level slog.Level, message string) {
	message = SanitizeMessage(message)
	at := time.Now()
	writeConfiguredLog(at, level, message)
	addToBufferAt(source, level, message, at)
}

func writeConfiguredLog(at time.Time, level slog.Level, message string) {
	backend, minLevel := currentLogConfig()
	if level >= minLevel {
		backend.Log(at, level, message)
	}
}
