package catalog

import "testing"

func TestWebDefaults(t *testing.T) {
	defaults := WebDefaults()
	if defaults[WebPortKey] != "2095" {
		t.Fatalf("web port default = %q", defaults[WebPortKey])
	}
	if defaults[WebPathKey] != "/app/" {
		t.Fatalf("web path default = %q", defaults[WebPathKey])
	}
}

func TestSessionDefaultsUseProvidedSecrets(t *testing.T) {
	defaults := SessionDefaults("secret-value", "salt-value")
	if defaults[SecretKey] != "secret-value" {
		t.Fatalf("secret default = %q", defaults[SecretKey])
	}
	if defaults[InstallSaltKey] != "salt-value" {
		t.Fatalf("install salt default = %q", defaults[InstallSaltKey])
	}
}

func TestRuntimeDefaults(t *testing.T) {
	defaults := RuntimeDefaults()
	if defaults[TimeLocationKey] != "Europe/Moscow" {
		t.Fatalf("time location default = %q", defaults[TimeLocationKey])
	}
	if defaults[ObservabilityMemoryCapMBKey] != "32" {
		t.Fatalf("memory cap default = %q", defaults[ObservabilityMemoryCapMBKey])
	}
}

func TestInternalDefaults(t *testing.T) {
	defaults := InternalDefaults("base-config")
	if defaults[ConfigKey] != "base-config" {
		t.Fatalf("config default = %q", defaults[ConfigKey])
	}
	if defaults[VersionKey] != "" {
		t.Fatalf("version default = %q", defaults[VersionKey])
	}
}
