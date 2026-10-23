package logging

import "context"

type dryValidationKey struct{}

// WithDryValidation retains log option validation but prevents a construct/close
// check from publishing runtime diagnostics or attaching output/file sinks.
// It is scoped to that validation context, never a process-global logging mode.
func WithDryValidation(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryValidationKey{}, true)
}
