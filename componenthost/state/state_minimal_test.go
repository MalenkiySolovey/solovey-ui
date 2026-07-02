//go:build minimal

package state

import "testing"

func TestMinimalProfileHasNoOptionalComponents(t *testing.T) {
	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 0 {
		t.Fatalf("minimal profile should not register optional components: %#v", components)
	}
}
