package parser

import (
	"encoding/json"
	"strings"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
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
		if outboundType == "" {
			continue
		}
		switch outboundType {
		case "direct", "block", "dns":
			continue
		default:
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
