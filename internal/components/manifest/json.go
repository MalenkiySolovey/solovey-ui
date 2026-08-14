package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func FromJSON(data []byte) (Manifest, error) {
	var parsed Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return Manifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("component manifest contains multiple JSON values")
	}
	if parsed.Version == "" || parsed.Since == "" {
		return Manifest{}, fmt.Errorf("component %q manifest version and since are required", parsed.ID)
	}
	if err := parsed.Validate(); err != nil {
		return Manifest{}, err
	}
	return parsed.Normalized(), nil
}

func MustFromJSON(data []byte) Manifest {
	parsed, err := FromJSON(data)
	if err != nil {
		panic(fmt.Errorf("component manifest: %w", err))
	}
	return parsed
}
