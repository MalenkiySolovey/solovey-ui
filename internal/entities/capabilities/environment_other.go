//go:build !linux

package capabilities

func observeResolve1() (bool, Resolve1NameState) { return false, Resolve1NameUnknown }
