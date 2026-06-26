package logging

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

func TestNewFactoryIgnoresUnsafeLogOutput(t *testing.T) {
	factory, err := NewFactory(log.Options{
		Options: option.LogOptions{Output: "/etc/cron.d/solovey-ui-pwn"},
	})
	if err != nil {
		t.Fatalf("NewFactory error: %v", err)
	}
	df, ok := factory.(*defaultFactory)
	if !ok {
		t.Fatalf("unexpected factory type %T", factory)
	}
	if df.filePath != "" {
		t.Fatalf("unsafe log.output should be ignored, got filePath %q", df.filePath)
	}
}

func TestNewFactoryKeepsSafeRelativeLogOutput(t *testing.T) {
	factory, err := NewFactory(log.Options{
		Options: option.LogOptions{Output: "box.log"},
	})
	if err != nil {
		t.Fatalf("NewFactory error: %v", err)
	}
	df, ok := factory.(*defaultFactory)
	if !ok {
		t.Fatalf("unexpected factory type %T", factory)
	}
	if df.filePath != "box.log" {
		t.Fatalf("safe relative path should be kept, got filePath %q", df.filePath)
	}
}

func TestObservableFactoryPublishesLogEntries(t *testing.T) {
	factory, err := NewFactory(log.Options{})
	if err != nil {
		t.Fatalf("NewFactory error: %v", err)
	}
	t.Cleanup(func() {
		_ = factory.Close()
	})

	observableFactory, ok := factory.(log.ObservableFactory)
	if !ok {
		t.Fatalf("factory does not implement ObservableFactory: %T", factory)
	}

	subscription, done, err := observableFactory.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	defer observableFactory.UnSubscribe(subscription)

	observableFactory.NewLogger("test").InfoContext(context.Background(), "hello")

	select {
	case entry := <-subscription:
		if entry.Level != log.LevelInfo {
			t.Fatalf("entry level = %v, want %v", entry.Level, log.LevelInfo)
		}
		if entry.Message != "hello" {
			t.Fatalf("entry message = %q, want hello", entry.Message)
		}
	case <-done:
		t.Fatal("subscription closed before receiving log entry")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observable log entry")
	}
}
