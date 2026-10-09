package canonical

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// HysteriaPorts normalizes Listable server_ports into the pinned core's colon
// grammar. Even a single port is n:n; the official ParsePorts requires a colon.
func HysteriaPorts(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	var items []any
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil, nil
		}
		items = []any{typed}
	case []string:
		for _, text := range typed {
			items = append(items, text)
		}
	case []any:
		items = typed
	default:
		return nil, errors.New("invalid Hysteria server_ports shape")
	}
	if len(items) > 64 {
		return nil, errors.New("Hysteria server_ports exceeds 64 segments")
	}
	ports := make([]string, 0, len(items))
	for _, item := range items {
		var text string
		switch typed := item.(type) {
		case string:
			text = typed
		case float64:
			if math.Trunc(typed) != typed || typed < 1 || typed > 65535 {
				return nil, errors.New("invalid Hysteria port")
			}
			text = strconv.Itoa(int(typed))
		case int:
			text = strconv.Itoa(typed)
		default:
			return nil, errors.New("invalid Hysteria port segment")
		}
		startText, endText, rangeSet := strings.Cut(text, ":")
		if !rangeSet {
			endText = startText
		}
		start, err := boundedPort(startText)
		if err != nil {
			return nil, err
		}
		end := 65535
		if endText != "" {
			end, err = boundedPort(endText)
			if err != nil {
				return nil, err
			}
		}
		if start > end {
			return nil, errors.New("reversed Hysteria port range")
		}
		// An empty upper bound is the official open-ended range, kept intact.
		endWire := strconv.Itoa(end)
		if rangeSet && endText == "" {
			endWire = ""
		}
		ports = append(ports, strconv.Itoa(start)+":"+endWire)
	}
	return ports, nil
}

func boundedPort(text string) (int, error) {
	if text == "" {
		return 0, errors.New("empty Hysteria port segment")
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid Hysteria port segment")
		}
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("Hysteria port outside 1..65535")
	}
	return port, nil
}

func HysteriaSharePorts(value any) (string, error) {
	ports, err := HysteriaPorts(value)
	if err != nil {
		return "", err
	}
	for i, port := range ports {
		start, end, _ := strings.Cut(port, ":")
		if start == end {
			ports[i] = start
		}
	}
	return strings.Join(ports, ","), nil
}

func ParseHysteriaSharePorts(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	segments := strings.Split(value, ",")
	for i, segment := range segments {
		if strings.Contains(segment, "-") {
			segment = strings.ReplaceAll(segment, "-", ":")
		}
		segments[i] = segment
	}
	return HysteriaPorts(segments)
}
