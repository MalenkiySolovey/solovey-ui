package jsonvalue

import "testing"

func TestOptionalJSONShapes(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null"), []byte(`{}`)} {
		if err := OptionalObject("object", raw); err != nil {
			t.Fatalf("OptionalObject(%q): %v", raw, err)
		}
	}
	if err := OptionalObject("object", []byte(`[]`)); err == nil {
		t.Fatal("OptionalObject accepted an array")
	}
	for _, raw := range [][]byte{nil, []byte("null"), []byte(`[]`)} {
		if err := OptionalArray("array", raw); err != nil {
			t.Fatalf("OptionalArray(%q): %v", raw, err)
		}
	}
	if err := OptionalArray("array", []byte(`{}`)); err == nil {
		t.Fatal("OptionalArray accepted an object")
	}
}
