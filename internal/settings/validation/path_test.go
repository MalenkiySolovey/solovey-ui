package validation

import (
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestNormalizeURLPath(t *testing.T) {
	cases := map[string]string{
		"":         "/",
		"sub":      "/sub/",
		"/sub":     "/sub/",
		"sub/path": "/sub/path/",
	}
	for input, want := range cases {
		if got := NormalizeURLPath(input); got != want {
			t.Fatalf("NormalizeURLPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAndValidateURLPath(t *testing.T) {
	got, err := normalizeAndValidateURLPath("custom", []string{"/api/"})
	if err != nil {
		t.Fatalf("NormalizeAndValidateURLPath returned error: %v", err)
	}
	if got != "/custom/" {
		t.Fatalf("normalized path = %q", got)
	}
	if _, err := normalizeAndValidateURLPath("/api/test", []string{"/api/"}); err == nil {
		t.Fatal("reserved path succeeded, want error")
	}
}

func TestSubscriptionFormatPathSettingsAreRecognized(t *testing.T) {
	for _, key := range []string{
		settingcatalog.SubJsonPathKey,
		settingcatalog.SubClashPathKey,
		settingcatalog.SubXrayPathKey,
	} {
		if !IsPathSetting(key) {
			t.Fatalf("%s should be recognized as a path setting", key)
		}
		if _, err := NormalizeAndValidatePathSetting(key, "/"+key+"/"); err != nil {
			t.Fatalf("%s should accept its own normalized path: %v", key, err)
		}
	}
	if _, err := NormalizeAndValidatePathSetting(settingcatalog.SubXrayPathKey, "/xray/"); err != nil {
		t.Fatalf("default xray path should be accepted for its own setting: %v", err)
	}
}
