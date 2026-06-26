package validation

import (
	"strings"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func NormalizeURLPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func normalizeAndValidateURLPath(path string, reserved []string) (string, error) {
	path = NormalizeURLPath(path)
	if err := validatePath(path, reserved); err != nil {
		return "", err
	}
	return path, nil
}

func IsPathSetting(key string) bool {
	switch key {
	case settingcatalog.WebPathKey, settingcatalog.SubPathKey, settingcatalog.SubJsonPathKey, settingcatalog.SubClashPathKey, settingcatalog.SubXrayPathKey:
		return true
	default:
		return false
	}
}

func NormalizeAndValidatePathSetting(key string, path string) (string, error) {
	return normalizeAndValidateURLPath(path, reservedPathPrefixesForSetting(key))
}

func reservedPathPrefixesForSetting(key string) []string {
	ownPrefix := ""
	switch key {
	case settingcatalog.SubPathKey:
		ownPrefix = "/sub/"
	case settingcatalog.SubJsonPathKey:
		ownPrefix = "/json/"
	case settingcatalog.SubClashPathKey:
		ownPrefix = "/clash/"
	case settingcatalog.SubXrayPathKey:
		ownPrefix = "/xray/"
	}
	if ownPrefix == "" {
		return reservedPathPrefixes
	}
	reserved := make([]string, 0, len(reservedPathPrefixes))
	for _, prefix := range reservedPathPrefixes {
		if prefix == ownPrefix {
			continue
		}
		reserved = append(reserved, prefix)
	}
	return reserved
}

func urlPathHasPrefix(path string, prefix string) bool {
	path = NormalizeURLPath(path)
	prefix = NormalizeURLPath(prefix)
	return path == prefix || strings.HasPrefix(path, prefix)
}
