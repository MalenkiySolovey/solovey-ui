package validation

import "testing"

func TestValidateOptionalHTTPURL(t *testing.T) {
	valid := []string{
		"",
		" https://example.com/profile?x=1 ",
		"http://example.com/path",
	}
	for _, value := range valid {
		if err := ValidateOptionalHTTPURL(value); err != nil {
			t.Fatalf("ValidateOptionalHTTPURL(%q) returned error: %v", value, err)
		}
	}

	invalid := []string{
		"ftp://example.com",
		"https://user:pass@example.com/profile",
		"https://example.com/path#fragment",
		"https://example.com/path\nx",
		"https://127.0.0.1/path",
		"https://10.0.0.1/path",
	}
	for _, value := range invalid {
		if err := ValidateOptionalHTTPURL(value); err == nil {
			t.Fatalf("ValidateOptionalHTTPURL(%q) succeeded, want error", value)
		}
	}
}
