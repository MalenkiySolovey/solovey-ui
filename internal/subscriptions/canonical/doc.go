// Package canonical stores the format-neutral view of subscription
// connections. Parsers and renderers keep their own package boundaries; this
// package only describes what was learned about a connection and how multiple
// observations of the same connection should be merged.
package canonical
