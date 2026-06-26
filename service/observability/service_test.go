package observability

import (
	"testing"
	"time"
)

type fixedSettings struct{ capMB int }

func (s *fixedSettings) GetObservabilityMemoryCapMB() (int, error) { return s.capMB, nil }

func resetStore(t testing.TB) {
	t.Helper()
	oldHistory := observabilityHistory
	oldCache := observabilityMemoryCapCache
	observabilityHistory = newObservabilityStore()
	observabilityMemoryCapCache = newObservabilityMemoryCapCache(time.Now)
	t.Cleanup(func() {
		observabilityHistory = oldHistory
		observabilityMemoryCapCache = oldCache
	})
}

func TestBucketsAreBoundedByDefault(t *testing.T) {
	resetStore(t)
	service := &Service{Settings: &fixedSettings{capMB: DefaultMemoryCapMB}}
	for i := 0; i < 350; i++ {
		if err := service.RecordObservabilitySample(ObservabilityBucket2s, testSample(i)); err != nil {
			t.Fatal(err)
		}
		if err := service.RecordCoreSample(ObservabilityBucket5m, CoreSample{DateTime: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	samples, err := service.HistoryForBucket(ObservabilityBucket2s)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != DefaultBucketCap(ObservabilityBucket2s) || samples[0].DateTime != 50 {
		t.Fatalf("unexpected bounded samples: len=%d first=%d", len(samples), samples[0].DateTime)
	}
	core, err := service.CoreHistoryForBucket(ObservabilityBucket5m)
	if err != nil {
		t.Fatal(err)
	}
	if len(core) != DefaultBucketCap(ObservabilityBucket5m) || core[0].DateTime != 206 {
		t.Fatalf("unexpected bounded core samples: len=%d first=%d", len(core), core[0].DateTime)
	}
	if _, err := service.HistoryForBucket(ObservabilityBucket("10s")); err == nil {
		t.Fatal("invalid bucket should be rejected")
	}
}

func TestMemoryCapShrinksBuckets(t *testing.T) {
	resetStore(t)
	service := &Service{Settings: &fixedSettings{capMB: 1}}
	expected := CapsForMemory(1)[ObservabilityBucket2s]
	for i := 0; i < 350; i++ {
		if err := service.RecordObservabilitySample(ObservabilityBucket2s, testSample(i)); err != nil {
			t.Fatal(err)
		}
	}
	samples, err := service.HistoryForBucket(ObservabilityBucket2s)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != expected || samples[0].DateTime != int64(350-expected) {
		t.Fatalf("unexpected memory-capped samples: len=%d first=%d", len(samples), samples[0].DateTime)
	}
}

func TestMemoryCapCacheRefreshesAfterTTL(t *testing.T) {
	resetStore(t)
	now := time.Unix(1_700_000_000, 0)
	observabilityMemoryCapCache = newObservabilityMemoryCapCache(func() time.Time { return now })
	settings := &fixedSettings{capMB: 1}
	service := &Service{Settings: settings}
	if err := service.RecordObservabilitySample(ObservabilityBucket2s, testSample(1)); err != nil {
		t.Fatal(err)
	}
	if observabilityHistory.lastAppliedMemoryCap != 1 {
		t.Fatalf("initial cap=%d, want 1", observabilityHistory.lastAppliedMemoryCap)
	}
	settings.capMB = DefaultMemoryCapMB
	now = now.Add(MemoryCapCacheTTL - time.Second)
	_ = service.RecordObservabilitySample(ObservabilityBucket2s, testSample(2))
	if observabilityHistory.lastAppliedMemoryCap != 1 {
		t.Fatal("memory cap refreshed before TTL")
	}
	now = now.Add(2 * time.Second)
	_ = service.RecordObservabilitySample(ObservabilityBucket2s, testSample(3))
	if observabilityHistory.lastAppliedMemoryCap != DefaultMemoryCapMB {
		t.Fatalf("cap did not refresh after TTL: %d", observabilityHistory.lastAppliedMemoryCap)
	}
}

func BenchmarkHistoryForBucketRead(b *testing.B) {
	resetStore(b)
	service := &Service{Settings: &fixedSettings{capMB: DefaultMemoryCapMB}}
	for i := 0; i < DefaultBucketCap(ObservabilityBucket2s); i++ {
		_ = service.RecordObservabilitySample(ObservabilityBucket2s, testSample(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := service.HistoryForBucket(ObservabilityBucket2s); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func testSample(i int) ObservabilitySample {
	return ObservabilitySample{DateTime: int64(i), CPU: float64(i)}
}
