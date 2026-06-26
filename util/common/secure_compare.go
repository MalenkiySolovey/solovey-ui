package common

import "crypto/subtle"

// ConstantTimeStringEqual compares strings across a fixed maximum length.
// Callers must choose maxLen at least as large as every accepted value.
func ConstantTimeStringEqual(a string, b string, maxLen int) int {
	var diff byte
	if len(a) != len(b) {
		diff = 1
	}
	for i := 0; i < maxLen; i++ {
		var av byte
		var bv byte
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		diff |= av ^ bv
	}
	return subtle.ConstantTimeByteEq(diff, 0)
}
