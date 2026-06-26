package ipcert

import (
	"testing"
	"time"
)

func TestShouldRenew(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		notAfter time.Time
		want     bool
	}{
		{"zero notAfter renews", time.Time{}, true},
		{"already expired renews", now.Add(-1 * time.Hour), true},
		{"71h remaining renews", now.Add(71 * time.Hour), true},
		{"exactly threshold does not renew", now.Add(RenewThreshold), false},
		{"73h remaining does not renew", now.Add(73 * time.Hour), false},
		{"fresh shortlived cert does not renew", now.Add(160 * time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRenew(tc.notAfter, now); got != tc.want {
				t.Fatalf("ShouldRenew = %v, want %v", got, tc.want)
			}
		})
	}
}

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

func TestValidateApplyTarget(t *testing.T) {
	ok := []string{"", "panel", "inbound:1", "inbound:42"}
	for _, v := range ok {
		if err := ValidateApplyTarget(v); err != nil {
			t.Errorf("ValidateApplyTarget(%q) = %v, want nil", v, err)
		}
	}
	bad := []string{"inbound:", "inbound:0", "inbound:-1", "inbound:abc", "service:1", "panel:1"}
	for _, v := range bad {
		if err := ValidateApplyTarget(v); err == nil {
			t.Errorf("ValidateApplyTarget(%q) = nil, want error", v)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	if err := ValidateEmail("", false); err != nil {
		t.Errorf("optional empty email = %v, want nil", err)
	}
	if err := ValidateEmail("", true); err == nil {
		t.Error("required empty email = nil, want error")
	}

	valid := []string{"admin@example.com", "a.b+tag@sub.example.co.uk"}
	for _, e := range valid {
		if err := ValidateEmail(e, true); err != nil {
			t.Errorf("ValidateEmail(%q) = %v, want nil", e, err)
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
		if err := ValidateEmail(e, false); err == nil {
			t.Errorf("ValidateEmail(%q) = nil, want error", e)
		}
	}
}

func TestValidatePort(t *testing.T) {
	for _, p := range []int{1, 80, 8080, 65535} {
		if err := ValidatePort(p); err != nil {
			t.Errorf("ValidatePort(%d) = %v, want nil", p, err)
		}
	}
	for _, p := range []int{0, -1, 65536} {
		if err := ValidatePort(p); err == nil {
			t.Errorf("ValidatePort(%d) = nil, want error", p)
		}
	}
}
