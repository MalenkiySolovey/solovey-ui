//go:build !minimal

package api

import (
	"encoding/json"
	"testing"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestPaginateOwnsEmptyCollectionJSONShape(t *testing.T) {
	for _, values := range [][]string{nil, {}} {
		items, total := paginate(values, protectionrepository.PageQuery{Page: 1, Limit: 1})
		if total != 0 || items == nil || len(items) != 0 {
			t.Fatalf("empty page = %#v total=%d; want non-nil empty collection", items, total)
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != "[]" {
			t.Fatalf("empty page JSON = %s; want []", encoded)
		}
	}
}

func TestPaginateOwnsPastEndPageJSONShape(t *testing.T) {
	items, total := paginate([]string{"one"}, protectionrepository.PageQuery{Page: 2, Limit: 1})
	if total != 1 || items == nil || len(items) != 0 {
		t.Fatalf("past-end page = %#v total=%d; want non-nil empty page with stable total", items, total)
	}
}
