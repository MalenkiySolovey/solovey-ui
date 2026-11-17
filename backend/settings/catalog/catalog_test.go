package catalog

import (
	"reflect"
	"testing"
)

func TestMergeDefaultMapsRejectsDuplicateKeys(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate default setting key panic")
		}
	}()
	_ = MergeDefaultMaps(map[string]string{"a": "1"}, map[string]string{"a": "2"})
}

func TestCatalogCopiesInputAndReturnsSortedKeys(t *testing.T) {
	defaults := map[string]string{"b": "2", "a": "1"}
	internal := KeySet("b")
	catalog := New(defaults, internal)
	defaults["c"] = "3"
	internal["a"] = struct{}{}

	if got := catalog.Keys(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("keys = %#v, want sorted a/b", got)
	}
	if !catalog.Editable("a") {
		t.Fatal("key a should stay editable after mutating original internal set")
	}
	if catalog.Editable("b") {
		t.Fatal("internal key b should not be editable")
	}
	if catalog.Editable("missing") {
		t.Fatal("missing key should not be editable")
	}
}

func TestCatalogHideInternal(t *testing.T) {
	catalog := New(map[string]string{"shown": "1", "hidden": "2"}, KeySet("hidden"))
	values := map[string]string{"shown": "1", "hidden": "2"}
	catalog.HideInternal(values)

	if _, ok := values["hidden"]; ok {
		t.Fatalf("internal key was not hidden: %#v", values)
	}
	if values["shown"] != "1" {
		t.Fatalf("visible key was changed: %#v", values)
	}
}

func TestSortedKeys(t *testing.T) {
	if got := SortedKeys(map[string]string{"z": "", "a": ""}); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("sorted keys = %#v", got)
	}
}
