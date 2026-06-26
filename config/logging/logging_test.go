package logging

import "testing"

func TestGetLogLevelFallsBackForInvalidEnv(t *testing.T) {
	t.Setenv("SUI_DEBUG", "")
	t.Setenv("SUI_LOG_LEVEL", "verbose")

	if got := GetLogLevel(); got != Info {
		t.Fatalf("GetLogLevel() = %q, want %q", got, Info)
	}
}

func TestGetLogLevelNormalizesValidEnv(t *testing.T) {
	t.Setenv("SUI_DEBUG", "")
	t.Setenv("SUI_LOG_LEVEL", " WARN ")

	if got := GetLogLevel(); got != Warn {
		t.Fatalf("GetLogLevel() = %q, want %q", got, Warn)
	}
}

func TestIsSafeLogOutputPath(t *testing.T) {
	cases := []struct {
		output string
		want   bool
	}{
		{"", true},
		{"stdout", true},
		{"stderr", true},
		{"box.log", true},
		{"logs/box.log", true},
		{"my..log", true},
		{"/etc/cron.d/solovey-ui", false},
		{"/var/log/solovey-ui/box.log", false},
		{"../../etc/passwd", false},
		{"logs/../../../etc/passwd", false},
		{"a/../b", false},
		{"..\\..\\windows", false},
		{"C:\\Windows\\system32", false},
	}
	for _, tc := range cases {
		if got := IsSafeLogOutputPath(tc.output); got != tc.want {
			t.Errorf("IsSafeLogOutputPath(%q) = %v, want %v", tc.output, got, tc.want)
		}
	}
}
