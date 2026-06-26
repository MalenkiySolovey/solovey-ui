package common

// WipeBytes overwrites a byte slice in place when sensitive plaintext is no
// longer needed. It cannot erase copies made by callers or the runtime.
func WipeBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
