package netentity

import "testing"

func TestFetchUsersRejectsUnsupportedInboundTypeBeforeSQL(t *testing.T) {
	_, err := (&InboundService{}).fetchUsers(nil, "vmess'); DROP TABLE clients; --", map[string]interface{}{}, 1)
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
	if _, err := (&InboundService{}).fetchUsers(nil, inboundType, map[string]interface{}{}, 1); err == nil {
		t.Fatal("unexpected JSON field should be rejected before SQL execution")
	}
}
