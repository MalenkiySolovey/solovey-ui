package mapping

import (
	"encoding/json"
	"fmt"
)

func marshalJSON(value any) (json.RawMessage, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func decodeJSON(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}
