package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

func decodeJSONObject(data []byte) (map[string]interface{}, error) {
	var raw map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, errors.New("entity JSON must be an object")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("entity JSON contains multiple values")
	}
	return raw, nil
}

func optionalUint(raw map[string]interface{}, key string, destination *uint) error {
	value, exists := raw[key]
	if !exists {
		return nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return fmt.Errorf("entity field %s must be an unsigned integer", key)
	}
	parsed, err := strconv.ParseUint(number.String(), 10, 0)
	if err != nil {
		return fmt.Errorf("entity field %s must be an unsigned integer: %w", key, err)
	}
	*destination = uint(parsed)
	return nil
}

func optionalInt(raw map[string]interface{}, key string, destination *int) error {
	value, exists := raw[key]
	if !exists {
		return nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return fmt.Errorf("entity field %s must be an integer", key)
	}
	parsed, err := strconv.ParseInt(number.String(), 10, 0)
	if err != nil {
		return fmt.Errorf("entity field %s must be an integer: %w", key, err)
	}
	*destination = int(parsed)
	return nil
}

func optionalInt64(raw map[string]interface{}, key string, destination *int64) error {
	value, exists := raw[key]
	if !exists {
		return nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return fmt.Errorf("entity field %s must be an integer", key)
	}
	parsed, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return fmt.Errorf("entity field %s must be an integer: %w", key, err)
	}
	*destination = parsed
	return nil
}
