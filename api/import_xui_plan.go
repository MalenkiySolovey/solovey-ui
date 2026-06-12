package api

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui"
)

func decodeXUIApplyPlan(upload *xuiUpload) (importxui.MigrationPlan, error) {
	if upload.PlanPath != "" {
		return decodeXUIApplyPlanFile(upload.PlanPath, upload.PlanSize)
	}
	return decodeXUIApplyPlanReader(strings.NewReader(upload.Fields["plan"]))
}

func decodeXUIApplyPlanFile(path string, size int64) (importxui.MigrationPlan, error) {
	var plan importxui.MigrationPlan
	// #nosec G304 -- path is created under the per-request upload temp directory.
	file, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	plan, err = decodeXUIApplyPlanReader(file)
	closeErr := file.Close()
	if err != nil {
		if size > maxXUIFieldBytes {
			return plan, &xuiFieldTooLargeError{Field: "plan", Limit: maxXUIFieldBytes}
		}
		return plan, err
	}
	if closeErr != nil {
		return plan, closeErr
	}
	return plan, nil
}

func decodeXUIApplyPlanReader(reader io.Reader) (importxui.MigrationPlan, error) {
	var plan importxui.MigrationPlan
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	return plan, nil
}
