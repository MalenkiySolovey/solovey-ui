package local

import (
	"encoding/json"
	"strings"

	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

type Link struct {
	Type   string `json:"type"`
	Remark string `json:"remark"`
	URI    string `json:"uri"`
}

type LinkMode string

const (
	LinkModeExternal LinkMode = "external"
	LinkModeAll      LinkMode = "all"
)

type ExternalLinkFetcher func(rawURL string) (string, error)

func ResolveClientLinks(rawLinks json.RawMessage, mode LinkMode, clientInfo string) []string {
	return ResolveClientLinksWithFetcher(rawLinks, mode, clientInfo, subexternal.Fetch)
}

func ResolveClientLinksWithFetcher(rawLinks json.RawMessage, mode LinkMode, clientInfo string, fetch ExternalLinkFetcher) []string {
	var links []Link
	if err := json.Unmarshal(rawLinks, &links); err != nil {
		return nil
	}
	result := make([]string, 0, len(links))
	for _, link := range links {
		switch link.Type {
		case "external":
			result = append(result, link.URI)
		case "sub":
			if fetch == nil {
				continue
			}
			subLinks, err := fetch(link.URI)
			if err != nil {
				logger.Warning("sub: Error getting external subscription:", err)
				continue
			}
			result = append(result, strings.Split(subLinks, "\n")...)
		case "local":
			if mode == LinkModeAll {
				result = append(result, AddClientInfo(link.URI, clientInfo))
			}
		}
	}
	return result
}

func AddClientInfo(uri string, clientInfo string) string {
	if clientInfo == "" {
		return uri
	}
	protocol := strings.Split(uri, "://")
	if len(protocol) < 2 {
		return uri
	}
	switch protocol[0] {
	case "vmess":
		var vmessJSON map[string]interface{}
		config, err := uricodec.Decode(protocol[1])
		if err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		if err := json.Unmarshal(config, &vmessJSON); err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		ps, _ := vmessJSON["ps"].(string)
		vmessJSON["ps"] = ps + clientInfo
		result, err := json.MarshalIndent(vmessJSON, "", "  ")
		if err != nil {
			logger.Warning("sub: Error decoding vmess + clientInfo content:", err)
			return uri
		}
		return "vmess://" + uricodec.Encode(result)
	default:
		return uri + clientInfo
	}
}
