package manifest

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Delivery string

const (
	DeliveryInProcess Delivery = "in-process"
)

type Manifest struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Since          string   `json:"since,omitempty"`
	Delivery       Delivery `json:"delivery"`
	DefaultEnabled bool     `json:"defaultEnabled"`
	TokenScopes    []string `json:"tokenScopes,omitempty"`
	Frontend       Frontend `json:"frontend,omitempty"`
	CLI            Packages `json:"cli,omitempty"`
	Privileged     Packages `json:"privilegedBroker,omitempty"`
	Database       Database `json:"database,omitempty"`
}

type Frontend struct {
	Entries []string `json:"entries,omitempty"`
}

type Packages struct {
	Entries []string `json:"entries,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var tokenScopePattern = regexp.MustCompile(`^[a-z0-9-]+(?::[a-z0-9-]+)*$`)
var frontendEntryPattern = regexp.MustCompile(`^(frontend|src)/[A-Za-z0-9_./-]+\.(ts|vue)$`)
var packageEntryPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)
var databaseTablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)
var durableKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,127}$`)

const (
	maxManifestNameBytes     = 128
	maxManifestVersionBytes  = 64
	maxManifestSinceBytes    = 64
	maxManifestTokenScopes   = 128
	maxManifestFrontendFiles = 64
	maxManifestPackageFiles  = 32
)

func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("component id %q must match ^[a-z0-9-]+$", id)
	}
	return nil
}

func (m Manifest) Validate() error {
	if err := ValidateID(m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) != m.Name || m.Name == "" || len(m.Name) > maxManifestNameBytes ||
		!utf8.ValidString(m.Name) || strings.ContainsAny(m.Name, "\x00\r\n\t") {
		return fmt.Errorf("component %q name is required", m.ID)
	}
	if len(m.Version) > maxManifestVersionBytes || len(m.Since) > maxManifestSinceBytes ||
		strings.ContainsAny(m.Version, "\x00\r\n\t ") || strings.ContainsAny(m.Since, "\x00\r\n\t ") {
		return fmt.Errorf("component %q version metadata is invalid", m.ID)
	}
	if m.Delivery == "" {
		return fmt.Errorf("component %q delivery is required", m.ID)
	}
	switch m.Delivery {
	case DeliveryInProcess:
	default:
		return fmt.Errorf("component %q has unsupported delivery %q", m.ID, m.Delivery)
	}
	if len(m.TokenScopes) > maxManifestTokenScopes {
		return fmt.Errorf("component %q declares too many token scopes", m.ID)
	}
	seenScopes := make(map[string]struct{}, len(m.TokenScopes))
	for _, scope := range m.TokenScopes {
		if scope == "" {
			return fmt.Errorf("component %q token scope must not be empty", m.ID)
		}
		if !tokenScopePattern.MatchString(scope) {
			return fmt.Errorf("component %q token scope %q must use lowercase alphanumeric, dash and colon-separated segments", m.ID, scope)
		}
		if _, duplicate := seenScopes[scope]; duplicate {
			return fmt.Errorf("component %q token scope %q is duplicated", m.ID, scope)
		}
		seenScopes[scope] = struct{}{}
	}
	if len(m.Frontend.Entries) > maxManifestFrontendFiles {
		return fmt.Errorf("component %q declares too many frontend entries", m.ID)
	}
	seenEntries := make(map[string]struct{}, len(m.Frontend.Entries))
	for _, entry := range m.Frontend.Entries {
		if entry == "" {
			return fmt.Errorf("component %q frontend entry must not be empty", m.ID)
		}
		if !frontendEntryPattern.MatchString(entry) {
			return fmt.Errorf("component %q frontend entry %q must be a frontend/*.ts, frontend/*.vue, src/*.ts, or src/*.vue module", m.ID, entry)
		}
		if _, duplicate := seenEntries[entry]; duplicate {
			return fmt.Errorf("component %q frontend entry %q is duplicated", m.ID, entry)
		}
		seenEntries[entry] = struct{}{}
	}
	if err := validatePackageEntries(m.ID, "cli", m.CLI.Entries); err != nil {
		return err
	}
	if err := validatePackageEntries(m.ID, "privileged broker", m.Privileged.Entries); err != nil {
		return err
	}
	return m.Database.Validate(m.ID, m.Version)
}

func validatePackageEntries(componentID, surface string, entries []string) error {
	if len(entries) > maxManifestPackageFiles {
		return fmt.Errorf("component %q declares too many %s entries", componentID, surface)
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !packageEntryPattern.MatchString(entry) || strings.Contains(entry, "..") || strings.Contains(entry, "//") || strings.HasSuffix(entry, "/") {
			return fmt.Errorf("component %q %s entry %q is invalid", componentID, surface, entry)
		}
		if _, duplicate := seen[entry]; duplicate {
			return fmt.Errorf("component %q %s entry %q is duplicated", componentID, surface, entry)
		}
		seen[entry] = struct{}{}
	}
	return nil
}

// Normalized returns the canonical manifest stored in the installed-owner
// catalog. JSON and programmatic registrations have identical checksums.
func (m Manifest) Normalized() Manifest {
	m.Database = m.Database.Normalized(m.Version)
	return m
}
