package codec

import (
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const WebSocketProtocolHeader = "Sec-WebSocket-Protocol"

var ErrUnsupportedWebSocketHeader = errors.New("WebSocket URI early data requires Sec-WebSocket-Protocol")

// WebSocketEarlyData validates shared WebSocket metadata before projection.
func WebSocketEarlyData(transport map[string]any) (uint32, error) {
	for _, key := range []string{"path", "early_data_header_name"} {
		if value, present := transport[key]; present && value != nil {
			if _, ok := value.(string); !ok {
				return 0, errors.New("invalid WebSocket text metadata")
			}
		}
	}
	var max uint64
	if value, present := transport["max_early_data"]; present && value != nil {
		switch typed := value.(type) {
		case float64:
			if math.Trunc(typed) != typed || typed < 0 || typed > math.MaxUint32 {
				return 0, errors.New("invalid WebSocket max_early_data")
			}
			max = uint64(typed)
		case int:
			if typed < 0 || uint64(typed) > math.MaxUint32 {
				return 0, errors.New("invalid WebSocket max_early_data")
			}
			max = uint64(typed)
		case uint32:
			max = uint64(typed)
		default:
			return 0, errors.New("invalid WebSocket max_early_data")
		}
	}
	return uint32(max), nil
}

// WebSocketPath encodes only the witnessed ed extension. Other active early
// data headers cannot be represented by this URI format and are diagnosed.
func WebSocketPath(transport map[string]any) (string, error) {
	path, _ := transport["path"].(string)
	max, err := WebSocketEarlyData(transport)
	if err != nil {
		return "", err
	}
	_, existing, err := DecodeWebSocketPath(path)
	if err != nil {
		return "", err
	}
	if max == 0 {
		if existing > 0 {
			return "", errors.New("WebSocket path ed query conflicts with absent early data")
		}
		return path, nil
	}
	header, _ := transport["early_data_header_name"].(string)
	if !strings.EqualFold(header, WebSocketProtocolHeader) {
		return "", ErrUnsupportedWebSocketHeader
	}
	if existing > 0 {
		if existing != max {
			return "", errors.New("WebSocket path ed query conflicts with max_early_data")
		}
		return path, nil
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	if strings.HasSuffix(path, "?") || strings.HasSuffix(path, "&") {
		separator = ""
	}
	return path + separator + "ed=" + strconv.FormatUint(uint64(max), 10), nil
}

// DecodeWebSocketPath removes only ed; the other path/query bytes stay intact.
func DecodeWebSocketPath(path string) (string, uint32, error) {
	base, query, found := strings.Cut(path, "?")
	if !found {
		return path, 0, nil
	}
	parts := strings.Split(query, "&")
	kept := make([]string, 0, len(parts))
	var early uint64
	seen := false
	for _, part := range parts {
		key, value, _ := strings.Cut(part, "=")
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			return "", 0, errors.New("invalid WebSocket path query")
		}
		if decoded != "ed" {
			if _, err := url.QueryUnescape(value); err != nil {
				return "", 0, errors.New("invalid WebSocket path query")
			}
			kept = append(kept, part)
			continue
		}
		if seen {
			return "", 0, errors.New("duplicate WebSocket path ed query")
		}
		seen = true
		value, err = url.QueryUnescape(value)
		if err != nil {
			return "", 0, errors.New("invalid WebSocket path ed query")
		}
		early, err = strconv.ParseUint(value, 10, 32)
		if err != nil || early == 0 {
			return "", 0, errors.New("invalid WebSocket path ed query")
		}
	}
	if !seen {
		return path, 0, nil
	}
	if len(kept) > 0 {
		base += "?" + strings.Join(kept, "&")
	}
	return base, uint32(early), nil
}
