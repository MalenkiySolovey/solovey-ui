package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestManagedJobRetainsAdmissionAcrossItsContextLifetime(t *testing.T) {
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "job-generation.db")); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	proceed := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	t.Cleanup(resume)
	job := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{}), block: proceed}
	managed := newManagedJob(context.Background(), job)
	finished := make(chan struct{})
	go func() { managed.Run(); close(finished) }()
	<-job.start
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type maintenanceResult struct {
		owner *dbsqlite.Maintenance
		err   error
	}
	maintained := make(chan maintenanceResult, 1)
	go func() { owner, err := dbsqlite.BeginMaintenance(ctx); maintained <- maintenanceResult{owner, err} }()
	for {
		_, release, err := dbsqlite.AcquireOperation(ctx)
		if errors.Is(err, dbsqlite.ErrMaintenance) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		release()
		runtime.Gosched()
	}
	select {
	case got := <-maintained:
		if got.owner != nil {
			got.owner.End()
		}
		t.Fatalf("maintenance crossed running job: %v", got.err)
	default:
	}
	late := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{}), block: proceed}
	newManagedJob(context.Background(), late).Run()
	select {
	case <-late.start:
		t.Fatal("late managed job was admitted")
	default:
	}
	resume()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case got := <-maintained:
		if got.err != nil {
			t.Fatal(got.err)
		}
		got.owner.End()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
