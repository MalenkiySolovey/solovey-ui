package canonical

import "errors"

type ClashUDPPolicy string

const (
	ClashUDPDefault  ClashUDPPolicy = ""
	ClashUDPEnabled  ClashUDPPolicy = "true"
	ClashUDPDisabled ClashUDPPolicy = "false"
)

func ParseClashUDPPolicy(value string) (ClashUDPPolicy, error) {
	switch ClashUDPPolicy(value) {
	case ClashUDPDefault, ClashUDPEnabled, ClashUDPDisabled:
		return ClashUDPPolicy(value), nil
	default:
		return "", errors.New("clash UDP policy must be default, true, or false")
	}
}
