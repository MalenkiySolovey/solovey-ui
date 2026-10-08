package assembly

import (
	"fmt"
	"testing"
)

func TestPinnedCompatibilityCatalogueCoversEveryAcceptedGroup(t *testing.T) {
	groups := map[string]bool{}
	for _, fact := range EditorContract().CompatibilityCatalogue {
		if fact.Consumer == "" || fact.Classification == "" || fact.Policy == "" {
			t.Fatal("unclassified pinned consumer")
		}
		groups[fact.ID] = true
	}
	if len(groups) != 22 {
		t.Fatal("accepted catalogue group coverage changed")
	}
	for i := 1; i <= 22; i++ {
		if !groups[fmt.Sprintf("DEP-%02d", i)] {
			t.Fatal("accepted pinned group missing")
		}
	}
}
