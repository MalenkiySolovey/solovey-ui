package httpheader

import canonical "github.com/MalenkiySolovey/solovey-host/httpheader"

func Sanitize(value string, max int) string { return canonical.Sanitize(value, max) }
