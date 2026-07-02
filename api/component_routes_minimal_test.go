//go:build minimal

package api

import "testing"

func prepareComponentRouteMetadata(t *testing.T) {
	t.Helper()
}

func expectedOptionalAPIPostRoutes() []string {
	return nil
}

func expectedOptionalAPIGetRoutes() []string {
	return nil
}

func securityCSRFOptionalPostRoutes() []string {
	return nil
}

func securityAuthZOptionalRows() []securityAuthZRow {
	return nil
}
