//go:build !minimal

package telegram_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
)

func BenchmarkTelegramNotifier_Enqueue(b *testing.B) {
	b.Run("fake_send_success", func(b *testing.B) {
		var sent atomic.Int64
		notifier := telegramservice.NewNotifier(0, func(string) telegramservice.Result {
			sent.Add(1)
			return telegramservice.Result{Success: true}
		}, nil)
		job := telegramservice.Notification{Event: "benchmark", Text: "telegram benchmark"}
		b.ReportMetric(float64(telegramservice.QueueCapacity), "capacity")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			notifier.Enqueue(job)
		}
		b.StopTimer()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = notifier.Stop(ctx)
		b.ReportMetric(float64(sent.Load()), "sent")
	})

	b.Run("overflow_blocked_sender", func(b *testing.B) {
		release := make(chan struct{})
		var sent atomic.Int64
		var overflows atomic.Int64
		notifier := telegramservice.NewNotifier(0, func(string) telegramservice.Result {
			sent.Add(1)
			<-release
			return telegramservice.Result{Success: true}
		}, func(string, map[string]any) {
			overflows.Add(1)
		})
		job := telegramservice.Notification{Event: "benchmark", Text: "telegram benchmark"}
		b.ReportMetric(float64(telegramservice.QueueCapacity), "capacity")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			notifier.Enqueue(job)
		}
		b.StopTimer()
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = notifier.Stop(ctx)
		b.ReportMetric(float64(overflows.Load()), "overflows")
		b.ReportMetric(float64(sent.Load()), "sent")
	})
}

func TestTelegramNotifierOverflowAnchor(t *testing.T) {
	release := make(chan struct{})
	var overflows atomic.Int64
	notifier := telegramservice.NewNotifier(telegramservice.QueueCapacity, func(string) telegramservice.Result {
		<-release
		return telegramservice.Result{Success: true}
	}, func(string, map[string]any) {
		overflows.Add(1)
	})
	for i := 0; i < telegramservice.QueueCapacity+100; i++ {
		notifier.Enqueue(telegramservice.Notification{Event: "benchmark", Text: "benchmark"})
	}
	if got := overflows.Load(); got == 0 {
		close(release)
		t.Fatal("expected telegram notifier overflow under blocked sender")
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = notifier.Stop(ctx)
	t.Logf("telegram overflow anchor: overflows=%d capacity=%d", overflows.Load(), telegramservice.QueueCapacity)
}
