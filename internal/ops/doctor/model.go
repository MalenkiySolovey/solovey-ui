package doctor

import (
	"fmt"
	"time"
)

type Severity string

const (
	SeverityOK    Severity = "ok"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Item struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Action   string   `json:"action,omitempty"`
	Details  any      `json:"details,omitempty"`
}

type Report struct {
	Status     Severity `json:"status"`
	Summary    string   `json:"summary"`
	Items      []Item   `json:"items"`
	RanAt      int64    `json:"ranAt"`
	DurationMS int64    `json:"durationMs"`
}

func FinishReport(start time.Time, items []Item) Report {
	status := SeverityOK
	errors := 0
	warnings := 0
	for _, item := range items {
		switch item.Severity {
		case SeverityError:
			errors++
			status = SeverityError
		case SeverityWarn:
			warnings++
			if status != SeverityError {
				status = SeverityWarn
			}
		}
	}
	summary := "All checks passed"
	if errors > 0 {
		summary = fmt.Sprintf("%d error(s), %d warning(s)", errors, warnings)
	} else if warnings > 0 {
		summary = fmt.Sprintf("%d warning(s)", warnings)
	}
	return Report{
		Status:     status,
		Summary:    summary,
		Items:      items,
		RanAt:      time.Now().Unix(),
		DurationMS: time.Since(start).Milliseconds(),
	}
}

func OK(id, title, message string, details any) Item {
	return Item{ID: id, Title: title, Severity: SeverityOK, Message: message, Details: details}
}

func Warn(id, title, message, action string, details any) Item {
	return Item{ID: id, Title: title, Severity: SeverityWarn, Message: message, Action: action, Details: details}
}

func Error(id, title, message, action string, details any) Item {
	return Item{ID: id, Title: title, Severity: SeverityError, Message: message, Action: action, Details: details}
}
