//go:build shtorm_extended

package logging

import (
	"context"

	"github.com/sagernet/sing-box/log"
)

// The Panel log sink has no Notice level. Keep the native level for engine
// subscribers/formatters and project it to the existing informational sink.
func panelLevel(level log.Level) log.Level {
	if level == log.LevelNotice {
		return log.LevelInfo
	}
	return level
}

func (l *observableLogger) Notice(args ...any) {
	l.NoticeContext(context.Background(), args...)
}

func (l *observableLogger) NoticeContext(ctx context.Context, args ...any) {
	l.Log(ctx, log.LevelNotice, args)
}
