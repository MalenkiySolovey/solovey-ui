package singboxconfig

import (
	"encoding/json"
	"testing"
)

func TestValidateConfigLogOutput(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"no log block", `{"dns":{"servers":[]}}`, false},
		{"log without output", `{"log":{"level":"info"}}`, false},
		{"empty output", `{"log":{"output":""}}`, false},
		{"stderr sentinel", `{"log":{"output":"stderr"}}`, false},
		{"stdout sentinel", `{"log":{"output":"stdout"}}`, false},
		{"relative file", `{"log":{"output":"box.log"}}`, false},
		{"relative subdir", `{"log":{"output":"logs/box.log"}}`, false},
		{"absolute path rejected", `{"log":{"output":"/etc/cron.d/solovey-ui"}}`, true},
		{"traversal rejected", `{"log":{"output":"../../etc/passwd"}}`, true},
		{"volume path rejected", `{"log":{"output":"C:\\Windows\\system32\\x"}}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLogOutput(json.RawMessage(tc.config))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %s, got nil", tc.config)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.config, err)
			}
		})
	}
}
