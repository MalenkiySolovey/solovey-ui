package netentity

import "testing"

func TestFetchUsersRejectsUnsupportedInboundTypeBeforeSQL(t *testing.T) {
	_, err := (&InboundService{}).fetchUsersByCondition(nil, "vmess'); DROP TABLE clients; --", "1=1", map[string]interface{}{})
	if err == nil {
		t.Fatal("unsupported inbound type should be rejected before SQL execution")
	}
}

func TestFetchUsersRejectsUnexpectedJSONFieldBeforeSQL(t *testing.T) {
	const inboundType = "test-malicious-field"
	old, existed := userJSONField[inboundType]
	userJSONField[inboundType] = "vmess') FROM clients; --"
	t.Cleanup(func() {
		if existed {
			userJSONField[inboundType] = old
		} else {
			delete(userJSONField, inboundType)
		}
	})
	if _, err := (&InboundService{}).fetchUsersByCondition(nil, inboundType, "1=1", map[string]interface{}{}); err == nil {
		t.Fatal("unexpected JSON field should be rejected before SQL execution")
	}
}
