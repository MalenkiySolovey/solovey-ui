// Package diagnostics defines the immutable result shared by semantic owners
// and their save, upgrade, Doctor and editor consumers. It owns no state.
package diagnostics

import "fmt"

const (
	LosslessAutomatic   = "LOSSLESS_AUTOMATIC"
	AutomaticDiagnostic = "AUTOMATIC_WITH_EXPLICIT_DIAGNOSTIC"
	ManualRequired      = "MANUAL_REQUIRED"
	UnsupportedLegacy   = "UNSUPPORTED_LEGACY"
	Error               = "error"
	Warn                = "warn"
)

// Finding must contain fixed text and structural paths, never submitted values.
type Finding struct {
	Kind                   string `json:"kind"`
	Path                   string `json:"path"`
	Code                   string `json:"code"`
	Severity               string `json:"severity"`
	Message                string `json:"message"`
	MigrationOutcome       string `json:"migrationOutcome,omitempty"`
	AutomaticAvailable     bool   `json:"automaticAvailable,omitempty"`
	OperatorActionRequired bool   `json:"operatorActionRequired,omitempty"`
}

// CompatibilityFact is owner-provided presentation data for the pinned core.
// Classification describes a consumer; it never grants runtime authorization.
type CompatibilityFact struct {
	ID             string `json:"id"`
	Consumer       string `json:"consumer"`
	Classification string `json:"classification"`
	Policy         string `json:"policy"`
}

type Rejection struct{ Finding Finding }

func (e *Rejection) ReasonCode() string { return e.Finding.Code }

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s [%s]: %s", e.Finding.Path, e.Finding.Code, e.Finding.Message)
}

func FirstError(findings []Finding) error {
	for _, f := range findings {
		if f.Severity == Error {
			return &Rejection{Finding: f}
		}
	}
	return nil
}

// Outcome classifies a complete owner's finding set without changing its data.
func Outcome(findings []Finding) string {
	result := LosslessAutomatic
	for _, finding := range findings {
		if finding.Severity == Error {
			if finding.MigrationOutcome == UnsupportedLegacy {
				return UnsupportedLegacy
			}
			result = ManualRequired
		} else if result == LosslessAutomatic {
			result = AutomaticDiagnostic
		}
	}
	return result
}
