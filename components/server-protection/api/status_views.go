//go:build !minimal

package api

import (
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func optionalPort(port int) *int {
	if port < 1 || port > 65535 {
		return nil
	}
	return &port
}

func statusFrom(ok bool) string {
	if ok {
		return "ok"
	}
	return "degraded"
}

func collisionWarnings(values []protectionresources.Collision) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Code+": "+value.LeftResourceID+" / "+value.RightResourceID)
	}
	return result
}

func contributorWarnings(values []hostresources.ResourceError) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, "contributor "+value.Owner+" is degraded")
	}
	return result
}
