package backup

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRunImportPostActionsRunsInOrder(t *testing.T) {
	var got []string
	actions := []importPostAction{
		{stage: "first", run: func(context.Context) error {
			got = append(got, "first")
			return nil
		}},
		{stage: "second", run: func(context.Context) error {
			got = append(got, "second")
			return nil
		}},
	}
	if err := runImportPostActions(context.Background(), actions, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-action order=%v, want %v", got, want)
	}
}

func TestRunImportPostActionsRollsBackProtectedFailure(t *testing.T) {
	cause := errors.New("boom")
	actions := []importPostAction{
		{stage: "protected", rollbackOnError: true, run: func(context.Context) error {
			return cause
		}},
	}
	var rollbackStage string
	var rollbackCause error
	err := runImportPostActions(context.Background(), actions, func(stage string, err error) error {
		rollbackStage = stage
		rollbackCause = err
		return errors.New("rolled back")
	})
	if err == nil || err.Error() != "rolled back" {
		t.Fatalf("expected rollback error, got %v", err)
	}
	if rollbackStage != "protected" || !errors.Is(rollbackCause, cause) {
		t.Fatalf("rollback got stage=%q cause=%v", rollbackStage, rollbackCause)
	}
}

func TestRunImportPostActionsFinalFailureDoesNotRollback(t *testing.T) {
	cause := errors.New("restart failed")
	actions := []importPostAction{
		{stage: "restarting app", run: func(context.Context) error {
			return cause
		}},
	}
	rollbackCalled := false
	err := runImportPostActions(context.Background(), actions, func(stage string, err error) error {
		rollbackCalled = true
		return err
	})
	if err == nil || err.Error() != "Error restarting app: restart failed" {
		t.Fatalf("unexpected final action error: %v", err)
	}
	if rollbackCalled {
		t.Fatal("final post-action failure must not rollback committed import")
	}
}
