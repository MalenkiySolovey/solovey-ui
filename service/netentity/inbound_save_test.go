package netentity

import "testing"

func TestInboundSaveRejectsUnknownAction(t *testing.T) {
	if _, err := (&InboundService{}).applyInboundSave(inboundSaveRequest{action: "mystery"}); err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
}
