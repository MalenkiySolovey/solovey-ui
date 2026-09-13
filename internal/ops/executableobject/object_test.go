package executableobject

import "testing"

func TestPolicyShapeIsBounded(t *testing.T) {
	if (&Object{}).Identity() != (Identity{}) {
		t.Fatal("empty object returned a non-empty identity")
	}
	if (Policy{MaxBytes: 1}).ForbiddenMode != 0 {
		t.Fatal("policy unexpectedly gained implicit semantic fields")
	}
}
