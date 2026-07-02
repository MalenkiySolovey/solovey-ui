package validation

import (
	"testing"
)

func TestValidateProxyURLValue(t *testing.T) {
	if err := ValidateProxyURLValue("", "stored"); err != nil {
		t.Fatalf("empty proxy URL returned error: %v", err)
	}
	if err := ValidateProxyURLValue("stored", "stored"); err != nil {
		t.Fatalf("stored marker returned error: %v", err)
	}
	if err := ValidateProxyURLValue("http://8.8.8.8:8080", "stored"); err != nil {
		t.Fatalf("valid proxy URL returned error: %v", err)
	}
	if err := ValidateProxyURLValue("http://127.0.0.1:8080", "stored"); err == nil {
		t.Fatal("private proxy URL should be rejected")
	}
}
