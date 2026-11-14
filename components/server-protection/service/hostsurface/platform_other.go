//go:build !linux

package hostsurface

import (
	"context"
	"errors"

	hostfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
)

func observePlatform(context.Context, hostfacts.Limits) (PlatformSnapshot, error) {
	return PlatformSnapshot{}, errors.New("host surface collection is available only on Linux")
}
