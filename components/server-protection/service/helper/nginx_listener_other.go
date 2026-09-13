//go:build !linux

package helper

import (
	"context"
	"errors"
)

func platformNginxOwnsListeners(context.Context, []int, []NginxListener) error {
	return errors.New("nginx listener ownership verification requires Linux")
}
