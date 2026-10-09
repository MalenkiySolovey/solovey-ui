package entitytls

import (
	"encoding/pem"
	"errors"
	"maps"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

// NaivePublicTrust adapts one self-signed SPKI pin to the public certificate
// trust source actually consumed by pinned Naive/Cronet. It never exports a
// silently ignored pin, guesses a trust source, or changes durable TLS state.
func NaivePublicTrust(client map[string]any, server *model.Tls) (map[string]any, error) {
	value, present := client["certificate_public_key_sha256"]
	if !present || value == nil {
		return client, nil
	}
	var pins []string
	switch list := value.(type) {
	case []string:
		pins = list
	case []any:
		for _, item := range list {
			pin, ok := item.(string)
			if !ok {
				return nil, errors.New("invalid Naive certificate pin")
			}
			pins = append(pins, pin)
		}
	default:
		return nil, errors.New("invalid Naive certificate pin")
	}
	if len(pins) == 0 {
		return client, nil
	}
	if server == nil || len(pins) != 1 {
		return nil, errors.New("Naive JSON requires a trusted certificate profile; this runtime cannot enforce the configured SPKI pins")
	}
	certificate := parseLeafCert(certPEMFromTLS(decodeTLSMap(server.Server)))
	if certificate == nil {
		return nil, errors.New("Naive JSON cannot resolve the public certificate for its pin")
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}))
	if !certIsSelfSigned(publicPEM) || certPublicKeySHA256(publicPEM) != pins[0] {
		return nil, errors.New("Naive JSON public certificate does not match its configured self-signed pin")
	}
	result := maps.Clone(client)
	delete(result, "certificate_public_key_sha256")
	result["certificate"] = []string{publicPEM}
	return result, nil
}
