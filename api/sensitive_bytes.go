package api

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
