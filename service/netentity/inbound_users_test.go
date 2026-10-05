package netentity

import "testing"

func TestFetchUsersRejectsUnsupportedInboundTypeBeforeSQL(t *testing.T) {
	_, err := (&InboundService{}).fetchUsers(nil, "vmess'); DROP TABLE clients; --", map[string]interface{}{}, 1)
	if err == nil {
		t.Fatal("unsupported inbound type should be rejected before SQL execution")
	}
}
