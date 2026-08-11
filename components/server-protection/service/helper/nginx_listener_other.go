//go:build !linux

package helper

import "errors"

func platformNginxOwnsListeners([]int, []NginxListener) error {
	return errors.New("nginx listener ownership verification requires Linux")
}
