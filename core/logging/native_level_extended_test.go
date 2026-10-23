//go:build shtorm_extended

package logging

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/log"
)

func TestNativeNoticeUsesSharedRedactionAndAttachment(t *testing.T) {
	factory := NewDefaultFactory(context.Background(), log.Formatter{}, io.Discard, "").(*defaultFactory)
	defer factory.Close()
	writer := new(attachmentWriter)
	factory.AttachPlatformWriter(writer)
	factory.NewLogger("password=tag-notice-canary").Notice("password=notice-canary")
	if len(writer.messages) != 1 || strings.Contains(writer.messages[0], "notice-canary") {
		t.Fatal("native notice bypassed the shared log owner or redaction")
	}
	if panelLevel(log.LevelNotice) != log.LevelInfo {
		t.Fatal("native notice lost its informational Panel severity")
	}
}
