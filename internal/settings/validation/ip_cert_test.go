package validation

import (
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidateIssuableIP(t *testing.T) {
	rejected := []string{
		"",
		"not-an-ip",
		"10.0.0.1",
		"192.168.1.1",
		"172.16.5.5",
		"127.0.0.1",
		"::1",
		"169.254.169.254",
		"0.0.0.0",
		"224.0.0.1",
		"100.64.0.1",
		"192.0.2.1",
		"198.51.100.1",
		"203.0.113.1",
		"255.255.255.255",
		"198.18.0.1",
		"192.0.0.1",
		"fe80::1",
		"fc00::1",
		"fd12:3456:789a::1",
		"2001:db8::1",
		"ff02::1",
		"::",
	}
	for _, ip := range rejected {
		if err := ValidateIssuableIP(ip); err == nil {
			t.Errorf("ValidateIssuableIP(%q) = nil, want error", ip)
		}
	}

	accepted := []string{
		"93.184.216.34",
		"8.8.8.8",
		"2606:4700:4700::1111",
	}
	for _, ip := range accepted {
		if err := ValidateIssuableIP(ip); err != nil {
			t.Errorf("ValidateIssuableIP(%q) = %v, want nil", ip, err)
		}
	}
}

func TestValidateIPCertApplyTarget(t *testing.T) {
	ok := []string{"", "panel", "inbound:1", "inbound:42"}
	for _, v := range ok {
		if err := ValidateIPCertApplyTarget(v); err != nil {
			t.Errorf("ValidateIPCertApplyTarget(%q) = %v, want nil", v, err)
		}
	}
	bad := []string{"inbound:", "inbound:0", "inbound:-1", "inbound:abc", "service:1", "panel:1"}
	for _, v := range bad {
		if err := ValidateIPCertApplyTarget(v); err == nil {
			t.Errorf("ValidateIPCertApplyTarget(%q) = nil, want error", v)
		}
	}
}

func TestValidateIPCertEmail(t *testing.T) {
	if err := ValidateIPCertEmail("", false); err != nil {
		t.Errorf("optional empty email = %v, want nil", err)
	}
	if err := ValidateIPCertEmail("", true); err == nil {
		t.Error("required empty email = nil, want error")
	}

	valid := []string{"admin@example.com", "a.b+tag@sub.example.co.uk"}
	for _, e := range valid {
		if err := ValidateIPCertEmail(e, true); err != nil {
			t.Errorf("ValidateIPCertEmail(%q) = %v, want nil", e, err)
		}
	}

	invalid := []string{
		"@",
		"@example.com",
		"admin@",
		"plainaddress",
		"ad\nmin@example.com",
		"admin@exa\tmple.com",
		"two addr@a.com, b@b.com",
		"Display Name <a@b.com>",
		string(make([]byte, 260)) + "@x.com",
	}
	for _, e := range invalid {
		if err := ValidateIPCertEmail(e, false); err == nil {
			t.Errorf("ValidateIPCertEmail(%q) = nil, want error", e)
		}
	}
}

func TestValidateIPCertPort(t *testing.T) {
	for _, p := range []int{1, 80, 443, 8080, 65535} {
		if err := ValidateIPCertPort(p); err != nil {
			t.Errorf("ValidateIPCertPort(%d) = %v, want nil", p, err)
		}
	}
	for _, p := range []int{0, -1, 65536, 100000} {
		if err := ValidateIPCertPort(p); err == nil {
			t.Errorf("ValidateIPCertPort(%d) = nil, want error", p)
		}
	}
}

func TestValidateIPCertSettingInput(t *testing.T) {
	valid := map[string]string{
		settingcatalog.IPCertEnabledKey:       "true",
		settingcatalog.IPCertTargetIPKey:      "8.8.8.8",
		settingcatalog.IPCertEmailKey:         "admin@example.com",
		settingcatalog.IPCertChallengePortKey: "80",
		settingcatalog.IPCertApplyTargetKey:   "inbound:42",
	}
	for key, value := range valid {
		if err := ValidateIPCertSettingInput(key, value); err != nil {
			t.Errorf("ValidateIPCertSettingInput(%q, %q) = %v, want nil", key, value, err)
		}
	}

	invalid := map[string]string{
		settingcatalog.IPCertEnabledKey:       "yes",
		settingcatalog.IPCertTargetIPKey:      "127.0.0.1",
		settingcatalog.IPCertEmailKey:         "not-an-email",
		settingcatalog.IPCertChallengePortKey: "70000",
		settingcatalog.IPCertApplyTargetKey:   "inbound:0",
	}
	for key, value := range invalid {
		if err := ValidateIPCertSettingInput(key, value); err == nil {
			t.Errorf("ValidateIPCertSettingInput(%q, %q) = nil, want error", key, value)
		}
	}
}
