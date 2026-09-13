package privilegedbroker

// HostBootGeneration projects the broker's existing host identity without
// exposing its platform mechanism to lifecycle consumers.
func HostBootGeneration() (string, error) {
	identity, err := currentBootID()
	if err != nil {
		return "", err
	}
	return Digest([]byte("host-boot-generation/v1:" + identity)), nil
}
