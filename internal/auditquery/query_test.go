package auditquery

import "testing"

func TestQueryBoundsAndFilters(t *testing.T) {
	if value, err := Limit("999", 25, 200); err != nil || value != 200 {
		t.Fatalf("bounded limit = %d, %v", value, err)
	}
	if _, err := Limit("0", 25, 200); err == nil {
		t.Fatal("zero limit accepted")
	}
	if value, err := Event("login.failed:admin"); err != nil || value != "login.failed:admin" {
		t.Fatalf("event = %q, %v", value, err)
	}
	if _, err := Event("login failed"); err == nil {
		t.Fatal("unsafe event filter accepted")
	}
	if _, err := Severity("error"); err == nil {
		t.Fatal("unsupported severity accepted")
	}
	if _, err := UnixSeconds("since", "1e3"); err == nil {
		t.Fatal("non-decimal timestamp accepted")
	}
}
