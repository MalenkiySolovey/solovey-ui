package netentity

import "testing"

func TestTLSSaveRejectsUnknownAction(t *testing.T) {
	if err := (&TlsService{}).applyTLSSave(tlsSaveRequest{action: "mystery"}); err == nil {
		t.Fatal("unknown TLS action was accepted")
	}
}
