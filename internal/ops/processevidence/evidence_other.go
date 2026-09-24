//go:build !linux

package processevidence

import "errors"

func Observe(int) (Fact, error) { return Fact{}, errors.New("linux process evidence is unavailable") }
