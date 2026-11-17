package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
)

func TestTokenHTTPProjectionPreservesWireShapeWithoutStoredCredentials(t *testing.T) {
	for _, user := range []*model.User{nil, {Id: 7, SortOrder: 2, Username: "admin", Password: "private-password-hash", LastLogins: "[]",
		ForcePasswordReset: true, PasswordPolicyVersion: 3, PasswordHashVersion: 2, CredentialGeneration: 9, MFAGeneration: 11, PasswordChangedAt: 123}} {
		token := model.Tokens{Id: 4, Desc: "operator", Token: "masked-only", TokenHash: "private-token-hash", TokenPrefix: "public-prefix",
			Scope: "read", Enabled: true, Expiry: 456, CreatedAt: 100, UpdatedAt: 200, LastUsedAt: 300, LastUsedIP: "203.0.113.1", UserId: 7, User: user}
		before, err := json.Marshal(token)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(tokenProjection(token))
		if err != nil {
			t.Fatal(err)
		}
		var beforeValue, afterValue any
		if err := json.Unmarshal(before, &beforeValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(after, &afterValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeValue, afterValue) {
			t.Fatalf("token wire changed: before=%s after=%s", before, after)
		}
		for _, secret := range []string{"private-password-hash", "private-token-hash", "masked-only"} {
			if strings.Contains(string(after), secret) {
				t.Fatalf("stored credential leaked in token response: %s", after)
			}
		}
	}
}
