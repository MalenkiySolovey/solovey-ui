package sqlite

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestBatchSizeRespectsSQLiteVariableBudget(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "batch.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	batch := BatchSize(DB(), &model.Client{})
	columns := countModelColumns(DB(), &model.Client{})
	if columns <= 0 {
		t.Fatal("model.Client column count must be positive")
	}
	if batch < 1 {
		t.Fatalf("BatchSize returned %d", batch)
	}
	if got := batch * columns; got > sqliteVariableBudget {
		t.Fatalf("batch exceeds SQLite variable budget: batch=%d columns=%d placeholders=%d budget=%d", batch, columns, got, sqliteVariableBudget)
	}

	rows := make([]model.Client, batch)
	if err := DB().CreateInBatches(&rows, batch).Error; err != nil {
		t.Fatalf("CreateInBatches with safe batch failed: %v", err)
	}
}
