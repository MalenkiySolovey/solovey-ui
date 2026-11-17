package backup

import (
	"bytes"
	"io"
)

func IsSQLite(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	if _, err := file.Read(buf); err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}
