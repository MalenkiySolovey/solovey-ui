//go:build !linux

package processevidence

import "errors"

func Observe(int) (Fact, error) { return Fact{}, errors.New("Linux process evidence is unavailable") }
