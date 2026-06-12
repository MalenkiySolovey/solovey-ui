package service

import (
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util"
)

func isPathSetting(key string) bool {
	switch key {
	case "webPath", settingKeySubPath, settingKeySubJsonPath, settingKeySubClashPath:
		return true
	default:
		return false
	}
}

func normalizeAndValidatePathSetting(key string, path string) (string, error) {
	path = normalizeURLPath(path)
	if err := util.ValidatePath(path, reservedPathPrefixesForSetting(key)); err != nil {
		return "", err
	}
	return path, nil
}

func normalizeURLPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func reservedPathPrefixesForSetting(key string) []string {
	ownPrefix := ""
	switch key {
	case settingKeySubPath:
		ownPrefix = "/sub/"
	case settingKeySubJsonPath:
		ownPrefix = "/json/"
	case settingKeySubClashPath:
		ownPrefix = "/clash/"
	}
	if ownPrefix == "" {
		return util.ReservedPathPrefixes
	}
	reserved := make([]string, 0, len(util.ReservedPathPrefixes))
	for _, prefix := range util.ReservedPathPrefixes {
		if prefix == ownPrefix {
			continue
		}
		reserved = append(reserved, prefix)
	}
	return reserved
}

func pathHasPrefix(path string, prefix string) bool {
	path = normalizeURLPath(path)
	prefix = normalizeURLPath(prefix)
	return path == prefix || strings.HasPrefix(path, prefix)
}
