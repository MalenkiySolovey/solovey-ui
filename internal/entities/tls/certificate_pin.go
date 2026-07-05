package entitytls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

// ApplySelfSignedPublicKeyPin keeps client TLS options aligned with a local
// self-signed server certificate. It is intentionally conservative: CA-signed,
// missing, invalid, and Reality TLS configs are left without a pin.
func ApplySelfSignedPublicKeyPin(tls *model.Tls) bool {
	if tls == nil {
		return false
	}
	server := decodeTLSMap(tls.Server)
	pin := ""
	if !realityEnabled(server) {
		if certPEM := certPEMFromTLS(server); certIsSelfSigned(certPEM) {
			pin = certPublicKeySHA256(certPEM)
		}
	}

	client := decodeTLSMap(tls.Client)
	if client == nil {
		client = map[string]interface{}{}
	}
	before, _ := json.Marshal(client)
	if pin != "" {
		client["certificate_public_key_sha256"] = []string{pin}
		delete(client, "certificate")
		delete(client, "certificate_path")
	} else {
		delete(client, "certificate_public_key_sha256")
	}
	afterCompact, _ := json.Marshal(client)
	after, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return false
	}
	tls.Client = after
	return string(before) != string(afterCompact)
}

func decodeTLSMap(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded
}

func certPEMFromTLS(tlsConfig map[string]interface{}) string {
	if tlsConfig == nil {
		return ""
	}
	switch cert := tlsConfig["certificate"].(type) {
	case string:
		return cert
	case []string:
		if len(cert) > 0 {
			return strings.Join(cert, "\n")
		}
	case []interface{}:
		lines := make([]string, 0, len(cert))
		for _, line := range cert {
			if text, ok := line.(string); ok {
				lines = append(lines, text)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	if path, ok := tlsConfig["certificate_path"].(string); ok && path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- operator-configured certificate path.
		if err == nil {
			return string(data)
		}
	}
	return ""
}

func certIsSelfSigned(pemData string) bool {
	cert := parseLeafCert(pemData)
	if cert == nil {
		return false
	}
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

func certPublicKeySHA256(pemData string) string {
	cert := parseLeafCert(pemData)
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func parseLeafCert(pemData string) *x509.Certificate {
	rest := []byte(pemData)
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			return nil
		}
		rest = next
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil
		}
		return cert
	}
}

func realityEnabled(tlsConfig map[string]interface{}) bool {
	reality, ok := tlsConfig["reality"].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, _ := reality["enabled"].(bool)
	return enabled
}
