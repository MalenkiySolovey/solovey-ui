//go:build !linux

package processevidence

import "errors"

func ObserveDescriptorSnapshot(int) (DescriptorSnapshot, error) {
	return DescriptorSnapshot{}, errors.New("linux process descriptor evidence is unavailable")
}
