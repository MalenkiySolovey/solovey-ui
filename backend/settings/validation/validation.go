package validation

import (
	"errors"
	"fmt"
	"strconv"
)

func ValidateIntRange(key string, value string, min int, max int) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return fmt.Errorf("invalid setting %s: must be an integer in [%d, %d]", key, min, max)
	}
	return nil
}

func ValidateTransportMode(value string) error {
	switch value {
	case "proxy", "outbound":
		return nil
	default:
		return errors.New("transport mode must be 'proxy' or 'outbound'")
	}
}
