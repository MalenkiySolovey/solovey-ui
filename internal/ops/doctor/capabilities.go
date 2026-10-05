package doctor

import (
	"errors"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"gorm.io/gorm"
)

func CapabilityChecks(db *gorm.DB) []Item {
	rows, err := runtimeprojection.Stored(db)
	if err != nil {
		return []Item{Error("capability-read", "Runtime capabilities", "Stored entity capabilities are unavailable.", "Check entity storage and retry.", nil)}
	}
	return CapabilityFindings(rows)
}

func CapabilityFindings(rows []runtimeprojection.Entity) []Item {
	items := []Item{}
	for _, row := range rows {
		if row.Capability.Available {
			continue
		}
		items = append(items, Error(fmt.Sprintf("capability-%s-%d", row.Category, row.ID), "Unavailable historical entity",
			fmt.Sprintf("%s %q cannot run in this binary (%s).", row.Category, row.Tag, row.Capability.Reason),
			"Inspect or export the row; remove its references or replace it with an available type.", row))
	}
	return items
}

func ConfigBuildFailure(err error) Item {
	var rejected *runtimeprojection.Rejection
	var details any
	if errors.As(err, &rejected) {
		details = rejected.Report
	}
	return Error("config-build", "Build sing-box config", "Unable to build configuration.", "Fix database/config rows before restarting sing-box.", details)
}
