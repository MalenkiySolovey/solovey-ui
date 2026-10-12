package protocol

import (
	"errors"
	"net/url"
)

// ClientTransportFromInbound maps literal inbound paths to the escaped paths
// consumed by the pinned clients' net/url.setPath. It never mutates its input.
func ClientTransportFromInbound(value any) any {
	transport, ok := value.(map[string]any)
	if !ok {
		return value
	}
	switch transport["type"] {
	case "ws", "http", "httpupgrade":
	default:
		return value
	}
	path, ok := transport["path"].(string)
	if !ok {
		return value
	}
	client := make(map[string]any, len(transport))
	for key, value := range transport {
		client[key] = value
	}
	client["path"] = (&url.URL{Path: path}).EscapedPath()
	return client
}

// ClientTransportLiteralPath mirrors the pinned client's URLSetPath. Errors
// omit paths, which may contain a private routing token.
func ClientTransportLiteralPath(path string) (string, error) {
	literal, err := url.PathUnescape(path)
	if err != nil {
		return "", errors.New("invalid client transport path escape")
	}
	return literal, nil
}

// ClientTransportURLPath preserves native wire bytes for URL-parsing consumers
// such as Mihomo WS/HTTPUpgrade. Literal ? must not become a query separator.
func ClientTransportURLPath(path string) (string, error) {
	literal, err := ClientTransportLiteralPath(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Path: literal}).EscapedPath(), nil
}
