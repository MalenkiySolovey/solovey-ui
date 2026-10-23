package logging

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

func TestDryValidationLoggingIsContextScopedAndStillChecksLevel(t *testing.T) {
	parent := context.Background()
	ctx := WithDryValidation(parent)
	for _, output := range []string{"", "stderr", "stdout", "box.log", "/credential-sentinel.log"} {
		factory, err := NewFactory(log.Options{Context: ctx, Options: option.LogOptions{Output: output, Level: "info"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, runtimeSink := factory.(*defaultFactory); runtimeSink {
			t.Fatal("dry validation attached runtime logging sinks")
		}
		factory.Logger().Warn("credential-sentinel")
		if err := factory.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if factory, err := NewFactory(log.Options{Context: ctx, Options: option.LogOptions{Level: "invalid-level"}}); err == nil || factory != nil {
		t.Fatal("dry validation accepted an invalid enabled log level")
	}
	// Disabled logging keeps the same existing option behavior in either mode.
	for _, context := range []context.Context{parent, ctx} {
		factory, err := NewFactory(log.Options{Context: context, Options: option.LogOptions{Disabled: true, Level: "invalid-level"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := factory.Close(); err != nil {
			t.Fatal(err)
		}
	}
	factory, err := NewFactory(log.Options{Context: parent, Options: option.LogOptions{Output: "box.log", Level: "info"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = factory.Close() })
	if sink, ok := factory.(*defaultFactory); !ok || sink.filePath != "box.log" {
		t.Fatal("validation context changed ordinary runtime logging")
	}
}
