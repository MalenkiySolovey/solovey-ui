package logging

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
)

func TestPlatformAttachmentReplacementCloseAndSanitization(t *testing.T) {
	factory := NewDefaultFactory(context.Background(), log.Formatter{}, io.Discard, "").(*defaultFactory)
	first, second := new(attachmentWriter), new(attachmentWriter)
	factory.AttachPlatformWriter(first)
	factory.Logger().Info("first")
	factory.AttachPlatformWriter(second)
	factory.NewLogger("password=tag-canary").Error("password=message-canary")
	if len(first.messages) != 1 || len(second.messages) != 1 {
		t.Fatal("attachment replacement accumulated writers")
	}
	if strings.Contains(second.messages[0], "tag-canary") || strings.Contains(second.messages[0], "message-canary") {
		t.Fatal("attachment received unsanitized tag or message")
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	factory.AttachPlatformWriter(first)
	factory.Logger().Info("closed")
	if len(first.messages) != 1 || len(second.messages) != 1 {
		t.Fatal("closed generation dispatched a log")
	}
}

func TestPlatformWriterDispatchAndCloseSerialize(t *testing.T) {
	factory := NewDefaultFactory(context.Background(), log.Formatter{}, io.Discard, "").(*defaultFactory)
	writer := &attachmentWriter{entered: make(chan struct{}), release: make(chan struct{})}
	factory.AttachPlatformWriter(writer)
	dispatched := make(chan struct{})
	go func() { factory.Logger().Info("barrier"); close(dispatched) }()
	waitAttachment(t, writer.entered)
	closing := make(chan struct{})
	go func() { _ = factory.Close(); close(closing) }()
	// The dispatch owns the factory lock until its bounded callback returns.
	// Releasing the callback is a deterministic completion boundary.
	close(writer.release)
	waitAttachment(t, dispatched)
	waitAttachment(t, closing)
	if !factory.closed || factory.platformWriter != nil {
		t.Fatal("close retained attachment")
	}
}

type attachmentWriter struct {
	messages         []string
	entered, release chan struct{}
	once             sync.Once
}

func (w *attachmentWriter) DisableColors() bool { return true }
func (w *attachmentWriter) WriteMessage(_ log.Level, message string) {
	if w.entered != nil {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	w.messages = append(w.messages, message)
}
func waitAttachment(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("attachment barrier did not complete")
	}
}
