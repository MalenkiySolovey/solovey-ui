package capabilities

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
)

// ResolveResource augments a type fact with critical nested capability facts.
// It never rewrites desired options. Malformed supported options remain the
// responsibility of the existing candidate/native validator.
func ResolveResource(category, panelType string, options []byte) Fact {
	fact := Resolve(category, panelType)
	if !fact.Available || (category != "inbounds" && category != "outbounds") {
		return fact
	}
	side := "in"
	if category == "outbounds" {
		side = "out"
	}
	if fields := registry.CriticalProtocolFields()[panelType+"/"+side]; len(fields) != 0 {
		var values map[string]json.RawMessage
		if json.Unmarshal(options, &values) == nil {
			for key, raw := range values {
				for field, capability := range fields {
					if strings.EqualFold(key, field) && len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && !capability.Available {
						fact.Available = false
						fact.Reason = "PROTOCOL_FIELD_CAPABILITY_UNAVAILABLE"
						fact.RequiredProvider = capability.RequiredProvider
						return fact
					}
				}
			}
		}
	}
	switch panelType {
	case "vmess", "vless", "trojan":
		var nested struct {
			Transport struct {
				Type string `json:"type"`
			} `json:"transport"`
		}
		if json.Unmarshal(options, &nested) == nil && nested.Transport.Type != "" {
			transport := registry.ResolveTransport(nested.Transport.Type)
			if !transport.Available() {
				fact.Available = false
				fact.Reason = "TRANSPORT_CAPABILITY_UNAVAILABLE"
			}
		}
	}
	return fact
}
