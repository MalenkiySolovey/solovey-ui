package manifest

import (
	"fmt"
	"regexp"
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
	Database       Database `json:"database,omitempty"`
}

type Frontend struct {
	Entries []string `json:"entries,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var tokenScopePattern = regexp.MustCompile(`^[a-z0-9-]+(?::[a-z0-9-]+)*$`)
var frontendEntryPattern = regexp.MustCompile(`^(frontend|src)/[A-Za-z0-9_./-]+\.(ts|vue)$`)
var databaseTablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)
var durableKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,127}$`)

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
	if m.Name == "" {
		return fmt.Errorf("component %q name is required", m.ID)
	}
	if m.Delivery == "" {
		return fmt.Errorf("component %q delivery is required", m.ID)
	}
	switch m.Delivery {
	case DeliveryInProcess:
	default:
		return fmt.Errorf("component %q has unsupported delivery %q", m.ID, m.Delivery)
	}
	for _, scope := range m.TokenScopes {
		if scope == "" {
			return fmt.Errorf("component %q token scope must not be empty", m.ID)
		}
		if !tokenScopePattern.MatchString(scope) {
			return fmt.Errorf("component %q token scope %q must use lowercase alphanumeric, dash and colon-separated segments", m.ID, scope)
		}
	}
	for _, entry := range m.Frontend.Entries {
		if entry == "" {
			return fmt.Errorf("component %q frontend entry must not be empty", m.ID)
		}
		if !frontendEntryPattern.MatchString(entry) {
			return fmt.Errorf("component %q frontend entry %q must be a frontend/*.ts, frontend/*.vue, src/*.ts, or src/*.vue module", m.ID, entry)
		}
	}
	return m.Database.Validate(m.ID, m.Version)
}

// Normalized returns the canonical manifest stored in the installed-owner
// catalog. JSON and programmatic registrations have identical checksums.
func (m Manifest) Normalized() Manifest {
	m.Database = m.Database.Normalized(m.Version)
	return m
}
