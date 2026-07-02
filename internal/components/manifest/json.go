package manifest

import (
	"encoding/json"
	"fmt"
)

func FromJSON(data []byte) (Manifest, error) {
	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Manifest{}, err
	}
	if err := parsed.Validate(); err != nil {
		return Manifest{}, err
	}
	return parsed, nil
}

func MustFromJSON(data []byte) Manifest {
	parsed, err := FromJSON(data)
	if err != nil {
		panic(fmt.Errorf("component manifest: %w", err))
	}
	return parsed
}
