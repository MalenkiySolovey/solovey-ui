package canonical

import (
	"errors"
	"strings"
	"unicode"
)

// ShadowsocksPlugin is ordered SIP002 metadata, not executable management.
func ShadowsocksPlugin(outbound map[string]any) (string, string, error) {
	read := func(key string) (string, error) {
		value, present := outbound[key]
		if !present {
			return "", nil
		}
		text, ok := value.(string)
		if !ok || len(text) > 4096 || containsControl(text) {
			return "", errors.New("invalid Shadowsocks plugin metadata")
		}
		return text, nil
	}
	plugin, err := read("plugin")
	if err != nil {
		return "", "", err
	}
	opts, err := read("plugin_opts")
	if err != nil {
		return "", "", err
	}
	if plugin == "" && opts != "" {
		return "", "", errors.New("shadowsocks plugin options require a plugin name")
	}
	if len(plugin) > 128 || strings.ContainsAny(plugin, ";:=\\/") || strings.IndexFunc(plugin, unicode.IsSpace) >= 0 {
		return "", "", errors.New("invalid Shadowsocks plugin name")
	}
	for i := 0; i < len(opts); i++ {
		if opts[i] != '\\' {
			continue
		}
		i++
		if i == len(opts) || !strings.ContainsRune(":;=\\", rune(opts[i])) {
			return "", "", errors.New("invalid SIP002 plugin option escape")
		}
	}
	return plugin, opts, nil
}

func ParseShadowsocksPlugin(value string) (string, string, error) {
	plugin, opts, _ := strings.Cut(value, ";")
	return ShadowsocksPlugin(map[string]any{"plugin": plugin, "plugin_opts": opts})
}
