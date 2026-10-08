package entitytls

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/caddyserver/certmagic"
)

func ACMEAuthority(provider string) (string, error) {
	switch provider {
	case "", "letsencrypt":
		return certmagic.LetsEncryptProductionCA, nil
	case "zerossl":
		return certmagic.ZeroSSLProductionCA, nil
	}
	if strings.HasPrefix(provider, "https://") {
		return provider, nil
	}
	return "", errors.New("TLS_ACME_AUTHORITY_INVALID: select an accepted ACME authority")
}

type HTTPConsumer struct {
	URL    string
	Client json.RawMessage
}

// Native ACME resolves its own HTTP options directly through the core HTTP
// manager. An absent client is a new direct transport, not route's default
// rule-set client. Legacy inline ACME uses its original system HTTP adapter.
func NativeHTTPConsumers(options json.RawMessage) []HTTPConsumer {
	var fields struct {
		Provider        string          `json:"provider"`
		HTTPClient      json.RawMessage `json:"http_client"`
		ExternalAccount struct {
			KeyID string `json:"key_id"`
		} `json:"external_account"`
		DNS01 struct {
			Provider  string `json:"provider"`
			ServerURL string `json:"server_url"`
		} `json:"dns01_challenge"`
	}
	if json.Unmarshal(options, &fields) != nil {
		return nil
	}
	authority, err := ACMEAuthority(fields.Provider)
	if err != nil {
		return nil
	}
	consumers := []HTTPConsumer{{URL: authority, Client: fields.HTTPClient}}
	if fields.Provider == "zerossl" && fields.ExternalAccount.KeyID == "" {
		consumers = append(consumers, HTTPConsumer{URL: "https://api.zerossl.com/acme/eab-credentials-email", Client: fields.HTTPClient})
	}
	switch fields.DNS01.Provider {
	case "cloudflare":
		consumers = append(consumers, HTTPConsumer{URL: "https://api.cloudflare.com/client/v4", Client: fields.HTTPClient})
	case "acmedns":
		consumers = append(consumers, HTTPConsumer{URL: fields.DNS01.ServerURL, Client: fields.HTTPClient})
	}
	return consumers
}
