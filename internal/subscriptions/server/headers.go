package server

import "github.com/MalenkiySolovey/solovey-ui/internal/httpheader"

func SafeHeaders(headers []string, maxBytes int) []string {
	safe := make([]string, len(headers))
	for i, header := range headers {
		safe[i] = httpheader.Sanitize(header, maxBytes)
	}
	return safe
}
