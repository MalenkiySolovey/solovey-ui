package database

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunImportPostOpenHooksRunsInNameOrder(t *testing.T) {
	resetImportPostOpenHooksForTest(t)
	var got []string
	RegisterImportPostOpenHook("b", func(context.Context) error {
		got = append(got, "b")
		return nil
	})
	RegisterImportPostOpenHook("a", func(context.Context) error {
		got = append(got, "a")
		return nil
	})

	if err := runImportPostOpenHooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-open hook order=%v, want %v", got, want)
	}
}

func TestRunImportPostOpenHooksNamesFailure(t *testing.T) {
	resetImportPostOpenHooksForTest(t)
	RegisterImportPostOpenHook("service.example", func(context.Context) error {
		return errors.New("boom")
	})

	err := runImportPostOpenHooks(context.Background())
	if err == nil {
		t.Fatal("expected hook error")
	}
	if !strings.Contains(err.Error(), "service.example") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected hook error: %v", err)
	}
}

func resetImportPostOpenHooksForTest(t *testing.T) {
	t.Helper()
	importPostOpenHooks.Lock()
	old := importPostOpenHooks.byName
	importPostOpenHooks.byName = map[string]importPostOpenHook{}
	importPostOpenHooks.Unlock()
	t.Cleanup(func() {
		importPostOpenHooks.Lock()
		importPostOpenHooks.byName = old
		importPostOpenHooks.Unlock()
	})
}
