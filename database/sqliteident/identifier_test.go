package sqliteident

import "testing"

func TestValidAndQuote(t *testing.T) {
	for _, value := range []string{"table_name", "Column9", "_private"} {
		if !Valid(value) {
			t.Fatalf("Valid(%q) = false", value)
		}
	}
	for _, value := range []string{"", "9table", "table-name", "table.name", "table name"} {
		if Valid(value) {
			t.Fatalf("Valid(%q) = true", value)
		}
	}
	if got := Quote(`table"name`); got != `"table""name"` {
		t.Fatalf("Quote() = %q", got)
	}
}
