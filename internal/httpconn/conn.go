package httpconn

import (
	"context"
	"net"
)

type contextKey struct{}

func SaveContext(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, contextKey{}, conn)
}

func FromContext(ctx context.Context) (net.Conn, bool) {
	conn, ok := ctx.Value(contextKey{}).(net.Conn)
	return conn, ok
}
