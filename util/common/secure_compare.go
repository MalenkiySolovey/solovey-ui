package common

import backendcrypto "github.com/MalenkiySolovey/solovey-backend/security/crypto"

// ConstantTimeStringEqual preserves the existing bounded security comparison.
func ConstantTimeStringEqual(a, b string, maxLen int) int {
	return backendcrypto.ConstantTimeStringEqual(a, b, maxLen)
}
