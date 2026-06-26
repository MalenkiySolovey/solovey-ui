package mapping

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

type xuiInboundSettings struct {
	Clients    []xuiClientSetting `json:"clients"`
	Accounts   []xuiAccount       `json:"accounts"`
	Method     string             `json:"method"`
	Password   string             `json:"password"`
	Network    string             `json:"network"`
	Encryption string             `json:"encryption"`
}

type xuiClientSetting struct {
	Comment    string `json:"comment"`
	Email      string `json:"email"`
	Enable     *bool  `json:"enable"`
	ExpiryTime int64  `json:"expiryTime"`
	Flow       string `json:"flow"`
	ID         string `json:"id"`
	LimitIP    int    `json:"limitIp"`
	SubID      string `json:"subId"`
	TgID       any    `json:"tgId"`
	TotalGB    int64  `json:"totalGB"`
	Password   string `json:"password"`
	Security   string `json:"security"`
}

type xuiAccount struct {
	User     string `json:"user"`
	Pass     string `json:"pass"`
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type ClientRef struct {
	SrcInboundID int64
	DstInboundID uint
	SrcTag       string
	Protocol     string
	Email        string
	UUID         string
	Password     string
	Flow         string
	Comment      string
	Enable       bool
	HasEnable    bool
	ExpiryTime   int64
	TotalGB      int64
	LimitIP      int
	SubID        string
	TgID         string
}

type MappedInbound struct {
	Inbound    model.Inbound
	ClientRefs []ClientRef
	Warnings   []string
}

func MapInbound(row source.InboundRow, tlsID uint, reality *RealitySpec, server string) (*MappedInbound, error) {
	var settings xuiInboundSettings
	if err := decodeJSON(row.Settings, &settings); err != nil {
		return nil, fmt.Errorf("inbound %s settings: %w", row.Tag, err)
	}
	stream, err := parseStreamSettings(row)
	if err != nil {
		return nil, err
	}
	switch stream.Network {
	case "kcp", "mkcp", "quic":
		return &MappedInbound{Warnings: []string{fmt.Sprintf("inbound %s: transport %q is unsupported by phase 2 importer; skipped", row.Tag, stream.Network)}}, nil
	}

	inType := inboundType(row.Protocol)
	if inType == "" {
		return &MappedInbound{Warnings: []string{fmt.Sprintf("inbound %s: unsupported protocol %q; skipped", row.Tag, row.Protocol)}}, nil
	}
	if inType == "http" && len(settings.Accounts) == 0 {
		return &MappedInbound{Warnings: []string{fmt.Sprintf("inbound %s: http has no accounts; skipped", row.Tag)}}, nil
	}

	transport, transportWarnings := mapTransport("inbound", row.Tag, stream)
	tlsBlock, tlsWarnings := mapOutboundTLSBlock(stream, reality)
	warnings := append(transportWarnings, tlsWarnings...)
	if w := listenAddressWarning(row); w != "" {
		warnings = append(warnings, w)
	}

	options := map[string]any{
		"listen":      listenAddress(row.Listen),
		"listen_port": row.Port,
	}
	if transport != nil {
		options["transport"] = transport
	}
	flow := firstClientFlow(settings.Clients)
	switch inType {
	case "shadowsocks":
		method := firstNonEmpty(settings.Method, "none")
		options["method"] = method
		if settings.Password != "" {
			options["password"] = settings.Password
		}
	case "vmess":
		options["security"] = "auto"
	}

	optionsJSON, err := marshalJSON(options)
	if err != nil {
		return nil, err
	}
	outJSON, err := buildOutJson(inType, row.Tag, server, row.Port, tlsBlock, transport, flow)
	if err != nil {
		return nil, err
	}
	refs := clientRefsFromSettings(row, inType, settings)
	return &MappedInbound{
		Inbound: model.Inbound{
			Type:    inType,
			Tag:     row.Tag,
			TlsId:   tlsID,
			Addrs:   buildAddrs(),
			OutJson: outJSON,
			Options: optionsJSON,
		},
		ClientRefs: refs,
		Warnings:   warnings,
	}, nil
}

func inboundType(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless":
		return "vless"
	case "vmess":
		return "vmess"
	case "trojan":
		return "trojan"
	case "shadowsocks":
		return "shadowsocks"
	case "http":
		return "http"
	case "socks":
		return "socks"
	default:
		return ""
	}
}

func listenAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0.0.0.0"
	}
	return value
}

// listenAddressWarning flags inbounds bound to a concrete source-server address.
// Such an address (a specific NIC IP from the old host) usually does not exist
// on the destination server, so the migrated inbound would fail to start there.
// Wildcard binds ("", 0.0.0.0, ::) are host-independent and need no warning.
func listenAddressWarning(row source.InboundRow) string {
	switch strings.TrimSpace(row.Listen) {
	case "", "0.0.0.0", "::", "[::]":
		return ""
	default:
		return fmt.Sprintf("inbound %s: binds to source listen address %q which may not exist on this host; verify or clear it, otherwise the inbound will fail to start", row.Tag, strings.TrimSpace(row.Listen))
	}
}

func firstClientFlow(clients []xuiClientSetting) string {
	for _, client := range clients {
		if strings.TrimSpace(client.Flow) != "" {
			return strings.TrimSpace(client.Flow)
		}
	}
	return ""
}

func clientRefsFromSettings(row source.InboundRow, protocol string, settings xuiInboundSettings) []ClientRef {
	refs := make([]ClientRef, 0, len(settings.Clients)+len(settings.Accounts))
	for _, client := range settings.Clients {
		email := strings.TrimSpace(client.Email)
		if email == "" {
			continue
		}
		enable := row.Enable
		hasEnable := false
		if client.Enable != nil {
			enable = enable && *client.Enable
			hasEnable = true
		}
		ref := ClientRef{
			SrcInboundID: row.ID,
			SrcTag:       row.Tag,
			Protocol:     protocol,
			Email:        email,
			UUID:         strings.TrimSpace(client.ID),
			Password:     strings.TrimSpace(client.Password),
			Flow:         strings.TrimSpace(client.Flow),
			Comment:      client.Comment,
			Enable:       enable,
			HasEnable:    hasEnable || !row.Enable,
			ExpiryTime:   client.ExpiryTime,
			TotalGB:      client.TotalGB,
			LimitIP:      client.LimitIP,
			SubID:        strings.TrimSpace(client.SubID),
			TgID:         stringifyTgID(client.TgID),
		}
		refs = append(refs, ref)
	}
	for _, account := range settings.Accounts {
		user := firstNonEmpty(account.Email, account.User, account.Username)
		if user == "" {
			continue
		}
		refs = append(refs, ClientRef{
			SrcInboundID: row.ID,
			SrcTag:       row.Tag,
			Protocol:     protocol,
			Email:        user,
			Password:     firstNonEmpty(account.Pass, account.Password),
			Enable:       row.Enable,
			HasEnable:    !row.Enable,
		})
	}
	return refs
}

func stringifyTgID(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == 0 {
			return ""
		}
		return fmt.Sprintf("%.0f", v)
	case json.Number:
		return v.String()
	default:
		text := fmt.Sprint(v)
		if text == "0" {
			return ""
		}
		return strings.TrimSpace(text)
	}
}
