package crypto

import (
	crand "crypto/rand"
	"errors"
	"math/big"
)

var credentialAlphabet = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")

// SecureRandom returns exactly n alphanumeric characters from crypto/rand.
// Authentication credentials, tokens, salts, and session identifiers must use
// this function so entropy failure is returned instead of falling back to a
// predictable pseudo-random generator.
func SecureRandom(n int) (string, error) {
	if n <= 0 || len(credentialAlphabet) == 0 {
		return "", errors.New("secure random length is invalid")
	}
	result := make([]rune, n)
	maximum := big.NewInt(int64(len(credentialAlphabet)))
	for index := range result {
		value, err := crand.Int(crand.Reader, maximum)
		if err != nil {
			return "", err
		}
		result[index] = credentialAlphabet[int(value.Int64())]
	}
	return string(result), nil
}
