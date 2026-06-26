package singboxconfig

import (
	"encoding/json"

	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidateLogOutput(data json.RawMessage) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil
	}
	logRaw, ok := top["log"]
	if !ok {
		return nil
	}
	var logBlock struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(logRaw, &logBlock); err != nil {
		return err
	}
	if !configlogging.IsSafeLogOutputPath(logBlock.Output) {
		return common.NewError("log.output must be a relative path within the panel directory; absolute paths and '..' are not allowed")
	}
	return nil
}
