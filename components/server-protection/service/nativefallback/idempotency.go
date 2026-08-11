package nativefallback

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func nativeActionBinding(action, key, operationID string, operationRevision int, planDigest, providerRevision string) string {
	prefix := nativeActionBindingPrefix(action, key)
	body := strings.Join([]string{action, operationID, strconv.Itoa(operationRevision), planDigest, providerRevision}, "\x00")
	bodySum := sha256.Sum256([]byte(body))
	return prefix + hex.EncodeToString(bodySum[:])
}

func nativeActionBindingPrefix(action, key string) string {
	keySum := sha256.Sum256([]byte(key))
	return action + ":" + hex.EncodeToString(keySum[:]) + ":"
}

func nativeActionReplay(binding, action, key, operationID string, operationRevision int, planDigest, providerRevision string) (sameKey, exact bool) {
	parts := strings.Split(binding, ":")
	if len(parts) != 3 || parts[0] != action {
		return false, false
	}
	keySum := sha256.Sum256([]byte(key))
	sameKey = subtleHexEqual(parts[1], hex.EncodeToString(keySum[:]))
	if !sameKey {
		return false, false
	}
	return true, binding == nativeActionBinding(action, key, operationID, operationRevision, planDigest, providerRevision)
}

func subtleHexEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}
