package identity

import (
	_ "embed"
	"strings"
)

//go:embed version
var version string

//go:embed name
var name string

func GetVersion() string {
	return strings.TrimSpace(version)
}

func GetName() string {
	return strings.TrimSpace(name)
}
