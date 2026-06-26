package validation

import (
	"errors"
	"strings"
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidateEndpointSettingInputDomain(t *testing.T) {
	if err := ValidateEndpointSettingInput(settingcatalog.WebDomainKey, "example.com", nil); err != nil {
		t.Fatalf("valid domain returned error: %v", err)
	}
	err := ValidateEndpointSettingInput(settingcatalog.WebDomainKey, "bad/host", nil)
	if err == nil || !strings.Contains(err.Error(), settingcatalog.WebDomainKey) {
		t.Fatalf("invalid domain error = %v, want key-prefixed error", err)
	}
}

func TestValidateEndpointSettingInputCertificatePath(t *testing.T) {
	called := false
	fileExists := func(path string) error {
		called = true
		if path != "/missing/cert.pem" {
			t.Fatalf("fileExists path = %q", path)
		}
		return errors.New("missing")
	}
	err := ValidateEndpointSettingInput(settingcatalog.WebCertFileKey, "/missing/cert.pem", fileExists)
	if err == nil || !strings.Contains(err.Error(), "is not exists") {
		t.Fatalf("cert path error = %v, want missing file error", err)
	}
	if !called {
		t.Fatal("fileExists was not called")
	}

	called = false
	if err := ValidateEndpointSettingInput(settingcatalog.WebCertFileKey, "", fileExists); err != nil {
		t.Fatalf("empty cert path returned error: %v", err)
	}
	if called {
		t.Fatal("fileExists called for empty cert path")
	}
}

func TestValidateEndpointSettingInputPath(t *testing.T) {
	if err := ValidateEndpointSettingInput(settingcatalog.SubPathKey, "custom-sub", nil); err != nil {
		t.Fatalf("custom sub path returned error: %v", err)
	}
	if err := ValidateEndpointSettingInput(settingcatalog.WebPathKey, "/api/test", nil); err == nil {
		t.Fatal("reserved web path succeeded, want error")
	}
}

func TestEndpointSettingClassifiers(t *testing.T) {
	if !isCertificatePathSetting(settingcatalog.SubKeyFileKey) {
		t.Fatal("SubKeyFileKey is not classified as certificate path")
	}
	if !isDomainSetting(settingcatalog.SubDomainKey) {
		t.Fatal("SubDomainKey is not classified as domain")
	}
	if !IsPathSetting(settingcatalog.SubClashPathKey) {
		t.Fatal("SubClashPathKey is not classified as path")
	}
	if isDomainSetting("unknown") || isCertificatePathSetting("unknown") || IsPathSetting("unknown") {
		t.Fatal("unknown key was classified as a known endpoint/path setting")
	}
}
