package entitytls

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestNaivePublicTrustUsesOnlyMatchedPublicCertificate(t *testing.T) {
	certificate, pin := testSelfSignedCert(t)
	server := &model.Tls{Server: json.RawMessage(`{"certificate":` + mustJSON(t, certificate) + `}`)}
	client := map[string]any{"enabled": true, "server_name": "fixture", "certificate_public_key_sha256": []string{pin}}
	before, _ := json.Marshal(client)
	projected, err := NaivePublicTrust(client, server)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := projected["certificate_public_key_sha256"]; present {
		t.Fatal("ignored Naive pin remained in public JSON")
	}
	public := projected["certificate"].([]string)
	if len(public) != 1 || certPublicKeySHA256(public[0]) != pin || strings.Contains(public[0], "PRIVATE") {
		t.Fatal("public trust does not preserve the configured key")
	}
	after, _ := json.Marshal(client)
	if string(before) != string(after) {
		t.Fatal("projection changed durable trust")
	}
	for _, pins := range []any{[]string{"wrong"}, []string{pin, "other"}, true, []any{7}} {
		client["certificate_public_key_sha256"] = pins
		if _, err := NaivePublicTrust(client, server); err == nil {
			t.Fatal("unmatched/invalid pins silently ignored")
		}
	}
}
