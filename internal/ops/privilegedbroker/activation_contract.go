package privilegedbroker

import (
	"errors"
	"strconv"
	"strings"
)

func activatedDescriptorNames(listenPID, currentPID int, listenFDs, listenFDNames string) ([]string, error) {
	count, err := strconv.Atoi(listenFDs)
	if listenPID != currentPID || err != nil || count != 2 {
		return nil, errors.New("privileged broker requires exactly two systemd socket activation descriptors")
	}
	names := strings.Split(listenFDNames, ":")
	if len(names) != 2 || names[0] == names[1] || names[0] != "main" && names[0] != "proof" || names[1] != "main" && names[1] != "proof" {
		return nil, errors.New("privileged broker systemd socket names are invalid")
	}
	return names, nil
}
