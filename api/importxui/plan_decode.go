package importxui

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	dbimport "github.com/MalenkiySolovey/solovey-ui/database/importxui"
)

func decodeApplyPlan(upload *Upload) (dbimport.MigrationPlan, error) {
	if upload.PlanPath != "" {
		return decodeXUIApplyPlanFile(upload.PlanPath, upload.PlanSize)
	}
	return decodeXUIApplyPlanReader(strings.NewReader(upload.Fields["plan"]))
}

func decodeXUIApplyPlanFile(path string, size int64) (dbimport.MigrationPlan, error) {
	var plan dbimport.MigrationPlan
	// #nosec G304 -- path is created under the per-request upload temp directory.
	file, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	plan, err = decodeXUIApplyPlanReader(file)
	closeErr := file.Close()
	if err != nil {
		if size > MaxFieldBytes {
			return plan, &xuiFieldTooLargeError{Field: "plan", Limit: MaxFieldBytes}
		}
		return plan, err
	}
	if closeErr != nil {
		return plan, closeErr
	}
	return plan, nil
}

func decodeXUIApplyPlanReader(reader io.Reader) (dbimport.MigrationPlan, error) {
	var plan dbimport.MigrationPlan
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	return plan, nil
}
