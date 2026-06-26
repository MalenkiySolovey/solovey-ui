package validation

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidateIntRange(key string, value string, min int, max int) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return common.NewErrorf("invalid setting %s: must be an integer in [%d, %d]", key, min, max)
	}
	return nil
}

func ValidateTransportMode(value string) error {
	switch value {
	case "proxy", "outbound":
		return nil
	default:
		return common.NewError("transport mode must be 'proxy' or 'outbound'")
	}
}
