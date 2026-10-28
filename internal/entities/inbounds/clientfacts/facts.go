// Package clientfacts owns neutral inbound credential and delivery schema.
// It neither encodes links nor persists clients nor owns build availability.
package clientfacts

import (
	"sort"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
)

type Fact struct {
	Type      string
	UserField string
	JSON      bool
	URI       bool
	UDP       bool
	// SecretField is a scoped encrypted credential field. Empty retains the
	// accepted historical credential backend for protocols not yet migrated.
	SecretField string
}

// Each declaration is schema metadata. Runtime support is separately resolved
// against the serving inbound, never this server's outbound build composition.
var declarations = []Fact{
	{"mixed", "mixed", true, true, true, ""}, {"socks", "socks", true, true, true, ""}, {"http", "http", true, true, false, ""},
	{"shadowsocks", "shadowsocks", true, true, true, ""}, {"vmess", "vmess", true, true, true, ""},
	{"trojan", "trojan", true, true, true, ""}, {"naive", "naive", true, true, true, ""},
	{"hysteria", "hysteria", true, true, true, ""}, {"shadowtls", "shadowtls", true, false, false, ""},
	{"tuic", "tuic", true, true, true, ""}, {"hysteria2", "hysteria2", true, true, true, ""},
	{"vless", "vless", true, true, true, ""}, {"anytls", "anytls", true, true, true, ""},
	{"snell", "snell", true, false, true, ""},
	// Delivery is admitted separately after the canonical serializers qualify.
	{"mieru", "mieru", true, false, true, "password"},
	{"trusttunnel", "trusttunnel", true, false, true, "password"},
	{"ssh", "ssh", false, false, false, "password"},
	{"mtproxy", "mtproxy", false, false, false, "secret"},
	// Keys are an encrypted delivery bundle, never a serving users array.
	{"sudoku", "sudoku", false, false, true, "keys"},
}

// SupportsUDP is a portable protocol fact, narrowed by the selected outbound
// network/version. It never grants this server a runtime network capability.
func SupportsUDP(panelType string, outbound map[string]any) bool {
	fact, known := Resolve(panelType)
	if !known || !fact.UDP {
		return false
	}
	if panelType == "socks" {
		if version, _ := outbound["version"].(string); version == "4" || version == "4a" {
			return false
		}
	}
	switch network := outbound["network"].(type) {
	case string:
		return network == "" || network == "udp"
	case []string:
		if len(network) == 0 {
			return true
		}
		for _, value := range network {
			if value == "udp" {
				return true
			}
		}
		return false
	case []any:
		if len(network) == 0 {
			return true
		}
		for _, value := range network {
			if value == "udp" {
				return true
			}
		}
		return false
	case nil:
		return true
	default:
		return false
	}
}

func Facts() []Fact { return append([]Fact(nil), declarations...) }

func DeliveryOnly(panelType string) bool { return panelType == "sudoku" }

func Resolve(panelType string) (Fact, bool) {
	// This is a client credential variant selected by Shadowsocks options, not
	// a storage or official-core runtime discriminator.
	if panelType == "shadowsocks16" {
		panelType = "shadowsocks"
	}
	for _, fact := range declarations {
		if fact.Type == panelType {
			return fact, true
		}
	}
	return Fact{}, false
}

// UserField returns only the fixed field declared by the inbound schema.
// Untrusted type strings can never become an interpolated SQLite JSON path.
func UserField(panelType string) (string, bool) {
	if panelType == "shadowsocks16" {
		return "shadowsocks16", true
	}
	fact, ok := Resolve(panelType)
	return fact.UserField, ok && fact.UserField != ""
}

// UserFieldForMethod selects the existing credential schema, independently
// of the official inbound discriminator or this server's build capabilities.
// All returned SQL fields remain fixed allow-listed strings.
func UserFieldForMethod(panelType, method string) (string, bool) {
	if panelType == "shadowsocks" && method == "2022-blake3-aes-128-gcm" {
		panelType = "shadowsocks16"
	}
	return UserField(panelType)
}

func CanDeliver(panelType, format string) bool {
	fact, ok := Resolve(panelType)
	if !ok || !entitycapabilities.Resolve("inbounds", panelType).Available {
		return false
	}
	switch format {
	case "json":
		return fact.JSON
	case "uri":
		return fact.URI
	default:
		return false
	}
}

func URITypes(availableOnly bool) []string {
	result := []string{}
	for _, fact := range declarations {
		if fact.URI && (!availableOnly || CanDeliver(fact.Type, "uri")) {
			result = append(result, fact.Type)
		}
	}
	sort.Strings(result)
	return result
}
