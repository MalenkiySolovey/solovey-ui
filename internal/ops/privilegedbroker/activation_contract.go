package privilegedbroker

import (
	"errors"
	"strconv"
	"strings"
)

const zapretDenyFDName = "zapret-deny"

func activatedDescriptorNames(listenPID, currentPID int, listenFDs, listenFDNames string) ([]string, error) {
	count, err := strconv.Atoi(listenFDs)
	if listenPID != currentPID || err != nil || count != 2 && !(zapretDenyActivationEnabled && count == 3) {
		return nil, errors.New("privileged broker systemd socket activation descriptor count or PID is invalid")
	}
	names := strings.Split(listenFDNames, ":")
	if len(names) != count {
		return nil, errors.New("privileged broker systemd socket names are invalid")
	}
	var main, proof, deny bool
	for _, name := range names {
		switch name {
		case "main":
			if !main {
				main = true
				continue
			}
		case "proof":
			if !proof {
				proof = true
				continue
			}
		case zapretDenyFDName:
			if zapretDenyActivationEnabled && !deny {
				deny = true
				continue
			}
		}
		return nil, errors.New("privileged broker systemd socket names are invalid")
	}
	if !main || !proof {
		return nil, errors.New("privileged broker requires both named systemd listeners")
	}
	return names, nil
}
