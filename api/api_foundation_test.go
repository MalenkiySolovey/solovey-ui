package api

import (
	"context"
	"strings"
	"testing"

	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
)

func TestValidateOutboundCheckTargetRejectsNonHTTPSAndPrivateIP(t *testing.T) {
	tests := []string{
		"http://example.com",
		"https://127.0.0.1/test",
		"https://10.0.0.1/test",
		"https://100.64.0.1/test",
		"https://[::1]/test",
		"https://user:pass@1.1.1.1/test",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			err := confighttp.ValidateOutboundCheckTarget(context.Background(), target)
			if err == nil {
				t.Fatal("expected target to be rejected")
			}
		})
	}
}

func TestValidateOutboundCheckTargetAcceptsPublicIP(t *testing.T) {
	err := confighttp.ValidateOutboundCheckTarget(context.Background(), "https://1.1.1.1/generate_204")
	if err != nil && strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("public IP should be allowed: %v", err)
	}
}
