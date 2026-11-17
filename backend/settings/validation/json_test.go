package validation

import "testing"

func TestValidateOptionalJSONValues(t *testing.T) {
	if err := ValidateOptionalJSONObject(`{"enabled":true}`, "obj"); err != nil {
		t.Fatalf("valid object rejected: %v", err)
	}
	if err := ValidateOptionalJSONArray(`[{"type":"rand"}]`, "arr"); err != nil {
		t.Fatalf("valid array rejected: %v", err)
	}
	if err := ValidateOptionalJSONObject("", "obj"); err != nil {
		t.Fatalf("empty object setting rejected: %v", err)
	}
	if err := ValidateOptionalJSONArray(" ", "arr"); err != nil {
		t.Fatalf("empty array setting rejected: %v", err)
	}
	if err := ValidateOptionalJSONObject(`[]`, "obj"); err == nil {
		t.Fatal("array accepted as object")
	}
	if err := ValidateOptionalJSONArray(`{}`, "arr"); err == nil {
		t.Fatal("object accepted as array")
	}
}
