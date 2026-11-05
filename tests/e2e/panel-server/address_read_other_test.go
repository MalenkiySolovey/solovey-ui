//go:build !windows

package main

func backendAddressSharingViolation(error) bool {
	return false
}
