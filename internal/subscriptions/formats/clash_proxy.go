package formats

import (
	"fmt"
	"strings"
)

func ensureUniqueClashProxyNames(proxies []interface{}) ([]string, map[string]string) {
	seen := make(map[string]bool, len(proxies))
	proxyTags := make([]string, 0, len(proxies))
	nameMap := make(map[string]string, len(proxies)*2)
	for index, raw := range proxies {
		proxy, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		base := strings.TrimSpace(asString(proxy["name"]))
		if base == "" {
			base = clashProxyFallbackName(proxy, index)
		}
		name := base
		for suffix := 2; seen[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		proxy["name"] = name
		seen[name] = true
		if base != "" {
			nameMap[base] = name
		}
		nameMap[name] = name
		proxyTags = append(proxyTags, name)
	}
	return proxyTags, nameMap
}
func clashProxyFallbackName(proxy map[string]interface{}, index int) string {
	proxyType := strings.TrimSpace(asString(proxy["type"]))
	server := strings.Trim(strings.TrimSpace(asString(proxy["server"])), "'")
	port := strings.TrimSpace(fmt.Sprint(proxy["port"]))
	if proxyType != "" && server != "" && port != "" && port != "<nil>" {
		return fmt.Sprintf("%s-%s-%s", proxyType, server, port)
	}
	return fmt.Sprintf("proxy-%d", index+1)
}
