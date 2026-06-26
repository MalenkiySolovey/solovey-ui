package scheduler

import (
	"github.com/MalenkiySolovey/solovey-ui/cronjob/internal/testutil"
	"testing"
	"time"
)

func TestCronJobStartRegistersJobsSynchronously(t *testing.T) {
	testutil.InitDatabase(t)

	c := New()
	if err := c.Start(time.UTC, 30); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)

	entries := c.cron.Entries()
	if len(entries) != 13 {
		t.Fatalf("expected 13 registered cron entries immediately after Start, got %d", len(entries))
	}
}
