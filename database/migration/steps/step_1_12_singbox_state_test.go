package steps

import "testing"

func TestSingBoxStoredStateChecksumIsReviewed(t *testing.T) {
	const reviewed = "1489bc32a649ff416774bac975a081e860d94bf12ee93c8857b98df7e988818b"
	if SingBoxStateChecksum != reviewed {
		t.Fatalf("1.12 stored-state contract changed: %s; review the boundary before updating its checksum", SingBoxStateChecksum)
	}
}
