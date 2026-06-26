package importxui

import (
	"encoding/json"
)

func marshalJSON(v any) (json.RawMessage, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}
