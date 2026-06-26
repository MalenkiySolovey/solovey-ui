package importxui

import (
	"bytes"
	"errors"
	"mime/multipart"
	"testing"
)

func TestReadXUIFieldReturnsNamedTooLargeError(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("plan", "abcd"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader := multipart.NewReader(&body, writer.Boundary())
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	got, err := readXUIField(part, "plan", 3)
	if err == nil {
		t.Fatalf("readXUIField returned value %q, want too-large error", got)
	}
	var tooLarge *xuiFieldTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error type=%T, want *xuiFieldTooLargeError", err)
	}
	if tooLarge.Field != "plan" || tooLarge.Limit != 3 {
		t.Fatalf("too-large error=%#v, want field plan limit 3", tooLarge)
	}
}
