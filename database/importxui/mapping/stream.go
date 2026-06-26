package mapping

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
)

type xuiStreamSettings struct {
	Network         string            `json:"network"`
	Security        string            `json:"security"`
	TLSSettings     xuiTLSSetting     `json:"tlsSettings"`
	RealitySettings xuiRealitySetting `json:"realitySettings"`
	TCPSettings     map[string]any    `json:"tcpSettings"`
	WSSettings      map[string]any    `json:"wsSettings"`
	GRPCSettings    map[string]any    `json:"grpcSettings"`
	HTTPSettings    map[string]any    `json:"httpSettings"`
	HTTPUPSettings  map[string]any    `json:"httpupgradeSettings"`
}

// xuiTLSSetting is the tlsSettings block of an Xray stream. Only the fields
// s-ui can carry over are decoded.
type xuiTLSSetting struct {
	ServerName    string           `json:"serverName"`
	AllowInsecure bool             `json:"allowInsecure"`
	Fingerprint   string           `json:"fingerprint"`
	ALPN          []string         `json:"alpn"`
	Certificates  []xuiCertificate `json:"certificates"`
}

func parseStreamSettings(row source.InboundRow) (xuiStreamSettings, error) {
	var stream xuiStreamSettings
	if len(row.StreamSettings) == 0 {
		return stream, nil
	}
	if err := json.Unmarshal(row.StreamSettings, &stream); err != nil {
		return stream, fmt.Errorf("inbound %s stream_settings: %w", row.Tag, err)
	}
	normalizeStreamSettings(&stream)
	return stream, nil
}

// parseOutboundStream decodes an Xray outbound's streamSettings into the shared
// xuiStreamSettings shape. An absent/invalid block yields a zero (tcp/none)
// stream because outbound import reports malformed proxy settings elsewhere.
func parseOutboundStream(ob xrayOutbound) xuiStreamSettings {
	var stream xuiStreamSettings
	if len(ob.StreamSettings) == 0 {
		return stream
	}
	if err := json.Unmarshal(ob.StreamSettings, &stream); err != nil {
		return xuiStreamSettings{}
	}
	normalizeStreamSettings(&stream)
	return stream
}

func normalizeStreamSettings(stream *xuiStreamSettings) {
	stream.Network = strings.ToLower(strings.TrimSpace(stream.Network))
	stream.Security = strings.ToLower(strings.TrimSpace(stream.Security))
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
