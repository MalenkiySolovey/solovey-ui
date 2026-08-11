package coreinboundcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const maxCanonicalJSONSize = 1 << 20

func canonicalJSON(content []byte) ([]byte, error) {
	if len(content) == 0 || len(content) > maxCanonicalJSONSize {
		return nil, fmt.Errorf("JSON size is outside the canonicalization bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	budget := canonicalSizeBudget{remaining: maxCanonicalJSONSize}
	value, err := canonicalValue(decoder, &budget)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

type canonicalSizeBudget struct {
	remaining int
}

func (b *canonicalSizeBudget) take(size int) error {
	if size < 0 || size > b.remaining {
		return fmt.Errorf("canonical JSON exceeds the size bound")
	}
	b.remaining -= size
	return nil
}

func canonicalValue(decoder *json.Decoder, budget *canonicalSizeBudget) ([]byte, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			if err := budget.take(2); err != nil {
				return nil, err
			}
			fields := make(map[string][]byte)
			keys := make([]string, 0)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				if _, exists := fields[key]; exists {
					return nil, fmt.Errorf("duplicate object key")
				}
				field, err := canonicalValue(decoder, budget)
				if err != nil {
					return nil, err
				}
				fields[key] = field
				keys = append(keys, key)
			}
			if closeToken, err := decoder.Token(); err != nil || closeToken != json.Delim('}') {
				return nil, fmt.Errorf("unterminated object")
			}
			sort.Strings(keys)
			var result bytes.Buffer
			result.WriteByte('{')
			for index, key := range keys {
				if index > 0 {
					if err := budget.take(1); err != nil {
						return nil, err
					}
					result.WriteByte(',')
				}
				encodedKey, _ := json.Marshal(key)
				if err := budget.take(len(encodedKey) + 1); err != nil {
					return nil, err
				}
				result.Write(encodedKey)
				result.WriteByte(':')
				result.Write(fields[key])
			}
			result.WriteByte('}')
			return result.Bytes(), nil
		case '[':
			if err := budget.take(2); err != nil {
				return nil, err
			}
			var result bytes.Buffer
			result.WriteByte('[')
			index := 0
			for decoder.More() {
				item, err := canonicalValue(decoder, budget)
				if err != nil {
					return nil, err
				}
				if index > 0 {
					if err := budget.take(1); err != nil {
						return nil, err
					}
					result.WriteByte(',')
				}
				result.Write(item)
				index++
			}
			if closeToken, err := decoder.Token(); err != nil || closeToken != json.Delim(']') {
				return nil, fmt.Errorf("unterminated array")
			}
			result.WriteByte(']')
			return result.Bytes(), nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter")
		}
	case json.Number:
		number, err := normalizeJSONNumber(value.String())
		if err != nil {
			return nil, err
		}
		if err := budget.take(len(number)); err != nil {
			return nil, err
		}
		return []byte(number), nil
	case string, bool, nil:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if err := budget.take(len(encoded)); err != nil {
			return nil, err
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value")
	}
}

func normalizeJSONNumber(value string) (string, error) {
	if len(value) > 4096 {
		return "", fmt.Errorf("JSON number is outside the canonicalization bound")
	}
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", strings.TrimPrefix(value, "-")
	}
	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		var err error
		exponent, err = strconv.Atoi(value[index+1:])
		if err != nil || exponent < -4096 || exponent > 4096 {
			return "", fmt.Errorf("JSON exponent is outside the canonicalization bound")
		}
		value = value[:index]
	}
	fractionDigits := 0
	if index := strings.IndexByte(value, '.'); index >= 0 {
		fractionDigits = len(value) - index - 1
		value = value[:index] + value[index+1:]
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0", nil
	}
	scale := exponent - fractionDigits
	for scale < 0 && strings.HasSuffix(value, "0") {
		value = strings.TrimSuffix(value, "0")
		scale++
	}
	if scale >= 0 {
		return sign + value + strings.Repeat("0", scale), nil
	}
	point := len(value) + scale
	if point > 0 {
		return sign + value[:point] + "." + value[point:], nil
	}
	return sign + "0." + strings.Repeat("0", -point) + value, nil
}

func canonicalDigest(content []byte) (string, error) {
	canonical, err := canonicalJSON(content)
	if err != nil {
		return "", err
	}
	return digestBytes(canonical), nil
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func digestValue(value any) string {
	content, _ := json.Marshal(value)
	return digestBytes(content)
}
