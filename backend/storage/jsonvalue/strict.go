package jsonvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const MaxStrictDepth = 256
const MaxStrictMembers = 1 << 20

// StrictObject rejects ambiguous JSON before any typed decoder can drop a
// duplicate member, replace invalid UTF-8 or accept a trailing document. It does
// not normalize, transform or return any user content.
func StrictObject(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(trimmed) {
		return errors.New("JSON object required")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	members := 0
	if err := strictValue(decoder, 0, &members); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("JSON object has trailing content")
	}
	return nil
}

func strictValue(decoder *json.Decoder, depth int, members *int) error {
	if depth > MaxStrictDepth || *members >= MaxStrictMembers {
		return errors.New("JSON shape exceeds its bounds")
	}
	*members++
	token, err := decoder.Token()
	if err != nil {
		return errors.New("JSON value is malformed")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	closing := json.Delim(']')
	if delimiter != '[' && delimiter != '{' {
		return errors.New("JSON container is malformed")
	}
	seen := map[string]struct{}{}
	if delimiter == '{' {
		closing = '}'
	}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return errors.New("JSON member is malformed")
			}
			name, ok := key.(string)
			if _, duplicate := seen[name]; !ok || duplicate {
				return errors.New("JSON member is ambiguous")
			}
			seen[name] = struct{}{}
		}
		if err := strictValue(decoder, depth+1, members); err != nil {
			return err
		}
	}
	if end, err := decoder.Token(); err != nil || end != closing {
		return errors.New("JSON container is not terminated")
	}
	return nil
}
