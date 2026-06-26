package logging

import (
	"context"
	"os"
	"time"

	suiLog "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/log"
	F "github.com/sagernet/sing/common/format"
)

type observableLogger struct {
	*defaultFactory
	tag string
}

func (l *observableLogger) Log(ctx context.Context, level log.Level, args []any) {
	level = log.OverrideLevelFromContext(level, ctx)
	if level > l.level {
		return
	}
	msg := F.ToString(args...)
	switch level {
	case log.LevelInfo:
		suiLog.CoreInfo(l.tag, msg)
	case log.LevelWarn:
		suiLog.CoreWarning(l.tag, msg)
	case log.LevelPanic:
	case log.LevelFatal:
	case log.LevelError:
		suiLog.CoreError(l.tag, msg)
	default:
		suiLog.CoreDebug(l.tag, msg)
	}
	l.observer.Emit(log.Entry{
		Level:   level,
		Message: msg,
	})
	if (l.filePath != "" || l.writer != os.Stderr) && l.writer != nil {
		message := l.formatter.Format(ctx, level, l.tag, msg, time.Now())
		_, _ = l.writer.Write([]byte(message))
	}
}

func (l *observableLogger) Trace(args ...any) {
	l.TraceContext(context.Background(), args...)
}

func (l *observableLogger) Debug(args ...any) {
	l.DebugContext(context.Background(), args...)
}

func (l *observableLogger) Info(args ...any) {
	l.InfoContext(context.Background(), args...)
}

func (l *observableLogger) Warn(args ...any) {
	l.WarnContext(context.Background(), args...)
}

func (l *observableLogger) Error(args ...any) {
	l.ErrorContext(context.Background(), args...)
}

func (l *observableLogger) Fatal(args ...any) {
	l.FatalContext(context.Background(), args...)
}

func (l *observableLogger) Panic(args ...any) {
	l.PanicContext(context.Background(), args...)
}

func (l *observableLogger) TraceContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelTrace, args)
}

func (l *observableLogger) DebugContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelDebug, args)
}

func (l *observableLogger) InfoContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelInfo, args)
}

func (l *observableLogger) WarnContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelWarn, args)
}

func (l *observableLogger) ErrorContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelError, args)
}

func (l *observableLogger) FatalContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelFatal, args)
}

func (l *observableLogger) PanicContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelPanic, args)
}
