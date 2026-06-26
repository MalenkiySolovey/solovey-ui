package validation

import "testing"

func TestValidatePathRejectsUnsafeInput(t *testing.T) {
	tests := []string{
		"relative/path",
		"/../app/",
		"/app//panel/",
		"/app:panel/",
		"/app*panel/",
		"/app\\panel/",
		"/app\x00panel/",
		"/app\npanel/",
		"/api/",
		"/api/settings",
		"/ws",
		"/ws/",
		"/ws/events/",
		"/assets/app.js",
		"/xray/",
		"/xray/client",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if err := validatePath(path, reservedPathPrefixes); err == nil {
				t.Fatal("expected path to be rejected")
			}
		})
	}
}

func TestValidatePathReportsReservedPrefix(t *testing.T) {
	err := validatePath("/assets/app.js", reservedPathPrefixes)
	if err == nil {
		t.Fatal("expected path to be rejected")
	}
	if err.Error() != "reserved path prefix: /assets/" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestValidatePathAcceptsSafeInput(t *testing.T) {
	tests := []string{
		"/",
		"/app/",
		"/panel-v2/",
		"/wsub/",
		"/ws-token/",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if err := validatePath(path, reservedPathPrefixes); err != nil {
				t.Fatalf("expected path to be accepted: %v", err)
			}
		})
	}
}
