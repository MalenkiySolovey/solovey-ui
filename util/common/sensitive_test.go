package common

import "testing"

func TestWipeBytesOverwritesSlice(t *testing.T) {
	value := []byte("secret")
	WipeBytes(value)
	for i, b := range value {
		if b != 0 {
			t.Fatalf("byte %d was not wiped: %d", i, b)
		}
	}
}
