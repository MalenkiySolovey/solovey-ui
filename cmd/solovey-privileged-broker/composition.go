package main

import (
	"errors"
	"strings"

	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	updatebroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

// runtimeComposition records independent owner-local selections injected by
// deployment assets. It is private composition-root wiring, not a platform
// identity and not a source of semantic defaults.
type runtimeComposition struct {
	transport         broker.TransportMode
	ssh               sshbroker.ResolvedSSHComposition
	deploymentBackend deploymentbroker.Backend
	updateMode        updatebroker.Mode
}

func parseRuntimeComposition(args []string) (runtimeComposition, error) {
	if len(args) != 6 {
		return runtimeComposition{}, errors.New("privileged broker requires exact deployment-selected owner composition")
	}
	sshArgs := make([]string, 0, 4)
	var deploymentBackendValue, updateModeValue string
	for _, argument := range args {
		if strings.HasPrefix(argument, "--deployment-backend=") {
			if deploymentBackendValue != "" {
				return runtimeComposition{}, errors.New("deployment broker backend argument is duplicated")
			}
			deploymentBackendValue = strings.TrimPrefix(argument, "--deployment-backend=")
			continue
		}
		if strings.HasPrefix(argument, "--update-mode=") {
			if updateModeValue != "" {
				return runtimeComposition{}, errors.New("update broker mode argument is duplicated")
			}
			updateModeValue = strings.TrimPrefix(argument, "--update-mode=")
			continue
		}
		sshArgs = append(sshArgs, argument)
	}
	transport, ssh, err := sshbroker.ParseRuntimeArgs(sshArgs)
	if err != nil {
		return runtimeComposition{}, err
	}
	deploymentBackend, err := deploymentbroker.ParseBackend(deploymentBackendValue)
	if err != nil {
		return runtimeComposition{}, err
	}
	updateMode, err := updatebroker.ParseMode(updateModeValue)
	if err != nil {
		return runtimeComposition{}, err
	}
	return runtimeComposition{
		transport:         transport,
		ssh:               ssh,
		deploymentBackend: deploymentBackend,
		updateMode:        updateMode,
	}, nil
}
