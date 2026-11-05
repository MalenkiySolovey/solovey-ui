package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds/composition"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
)

type LinkParser func(link string, index int) (*map[string]interface{}, string, error)

func ParseExternalOutbounds(data string, parseLink LinkParser) ([]map[string]interface{}, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("no result")
	}
	if strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}") {
		return parseSingBoxConfigOutbounds(data)
	}
	return parseExternalLinkOutbounds(data, parseLink)
}

func parseSingBoxConfigOutbounds(data string) ([]map[string]interface{}, error) {
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		logger.Warning("sub: Error unmarshalling JSON:", err)
		return nil, err
	}
	outbounds, ok := jsonData["outbounds"].([]any)
	if !ok {
		logger.Warning("sub: missing outbounds field")
		return nil, common.NewError("invalid subscription: missing outbounds")
	}

	result := make([]map[string]interface{}, 0, len(outbounds))
	for _, outbound := range outbounds {
		outboundMap, ok := outbound.(map[string]interface{})
		if !ok || len(outboundMap) == 0 {
			continue
		}
		outboundType := strings.TrimSpace(stringValue(outboundMap["type"]))
		if outboundType == "parser" {
			var err error
			outboundMap, err = expandParserOutbound(outboundMap)
			if err != nil {
				return nil, err
			}
			outboundType = stringValue(outboundMap["type"])
		}
		if outboundType == "" {
			continue
		}
		switch outboundType {
		case "direct", "block", "dns":
			continue
		default:
			raw, _ := json.Marshal(outboundMap)
			canonical, err := composition.CanonicalReferences(outboundType, raw)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(canonical, &outboundMap); err != nil {
				return nil, err
			}
			result = append(result, outboundMap)
		}
	}
	if len(result) == 0 {
		return nil, common.NewError("no result")
	}
	return result, nil
}

func parseExternalLinkOutbounds(data string, parseLink LinkParser) ([]map[string]interface{}, error) {
	return ParseExternalLinkOutbounds(data, parseLink)
}

func ParseExternalLinkOutbounds(data string, parseLink LinkParser) ([]map[string]interface{}, error) {
	if parseLink == nil {
		return nil, common.NewError("subscription link parser is not configured")
	}
	result := make([]map[string]interface{}, 0)
	for _, link := range strings.Split(data, "\n") {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		outbound, _, err := parseLink(link, 0)
		if err == nil && outbound != nil {
			result = append(result, *outbound)
		}
	}
	if len(result) == 0 {
		return nil, common.NewError("no result")
	}
	return result, nil
}

func ParseSingBoxOutbounds(data string) ([]map[string]interface{}, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("no result")
	}
	if !strings.HasPrefix(data, "{") || !strings.HasSuffix(data, "}") {
		return nil, common.NewError("not a sing-box json subscription")
	}
	return parseSingBoxConfigOutbounds(data)
}

// Parser is import syntax, not a second runtime/credential owner. Resolve it
// through the existing URI/EndpointIR path before the canonical import preview.
func expandParserOutbound(wrapper map[string]any) (map[string]any, error) {
	invalid := errors.New("PARSER_OUTBOUND_REQUIRES_EXPLICIT_MAPPING")
	link, ok := wrapper["link"].(string)
	if !ok || strings.TrimSpace(link) == "" {
		return nil, invalid
	}
	allowed := map[string]bool{"type": true, "tag": true, "link": true}
	for _, name := range jsonfields.Names(reflect.TypeFor[option.DialerOptions](), false) {
		allowed[name] = true
	}
	overrides := make(map[string]any)
	for field, value := range wrapper {
		if !allowed[field] || value == nil {
			return nil, invalid
		}
		if field != "type" && field != "tag" && field != "link" {
			overrides[field] = value
		}
	}
	raw, err := json.Marshal(overrides)
	if err != nil {
		return nil, invalid
	}
	var typed option.DialerOptions
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil {
		return nil, invalid
	}
	outbound, _, err := suburi.Parse(strings.TrimSpace(link), 0)
	if err != nil || outbound == nil || stringValue((*outbound)["type"]) == "" {
		return nil, invalid
	}
	for field, value := range overrides {
		(*outbound)[field] = value
	}
	if tag, exists := wrapper["tag"]; exists {
		if _, ok := tag.(string); !ok {
			return nil, invalid
		}
		(*outbound)["tag"] = tag
	}
	return *outbound, nil
}
