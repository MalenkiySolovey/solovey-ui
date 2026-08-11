package resources

import (
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func resource(id, protocol, listen string, port int) hostresources.ProtectableResource {
	return hostresources.ProtectableResource{ID: id, Protocol: protocol, Listen: listen, Port: port}
}

func TestDetectCollisions(t *testing.T) {
	tests := []struct {
		name     string
		items    []hostresources.ProtectableResource
		code     string
		severity CollisionSeverity
	}{
		{"two TCP listeners on one exact socket", []hostresources.ProtectableResource{resource("a", "stream", "127.0.0.1", 443), resource("b", "http", "127.0.0.1", 443)}, "listener_collision", CollisionError},
		{"wildcard versus exact listener", []hostresources.ProtectableResource{resource("a", "stream", "0.0.0.0", 443), resource("b", "stream", "203.0.113.4", 443)}, "wildcard_listener_collision", CollisionError},
		{"dual stack wildcard ambiguity", []hostresources.ProtectableResource{resource("a", "stream", "0.0.0.0", 443), resource("b", "stream", "::", 443)}, "dual_stack_ambiguous", CollisionWarning},
		{"fallback site versus public inbound 443 without fronting owner", []hostresources.ProtectableResource{resource("fallback", "http", "0.0.0.0", 443), resource("inbound", "stream", "0.0.0.0", 443)}, "listener_collision", CollisionError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DetectCollisions(test.items)
			if len(got) != 1 || got[0].Code != test.code || got[0].Severity != test.severity {
				t.Fatalf("collisions = %#v", got)
			}
		})
	}
}

func TestDetectCollisionsAllowsSeparateSocketAndDocumentedSharing(t *testing.T) {
	if got := DetectCollisions([]hostresources.ProtectableResource{resource("tcp", "stream", "0.0.0.0", 443), resource("udp", "udp", "0.0.0.0", 443)}); len(got) != 0 {
		t.Fatalf("TCP and UDP collided: %#v", got)
	}
	left := resource("panel", "http", "127.0.0.1", 2095)
	right := resource("site", "http", "127.0.0.1", 2095)
	left.Capabilities.RouteHints = []string{"shares-listener:web-publicsurface"}
	right.Capabilities.RouteHints = []string{"shares-listener:web-publicsurface"}
	if got := DetectCollisions([]hostresources.ProtectableResource{left, right}); len(got) != 0 {
		t.Fatalf("documented publicsurface sharing collided: %#v", got)
	}
	if got := DetectCollisions([]hostresources.ProtectableResource{resource("local", "stream", "127.0.0.1", 443), resource("public", "stream", "203.0.113.4", 443)}); len(got) != 0 {
		t.Fatalf("loopback/public split collided: %#v", got)
	}
	front := resource("fronting", "stream", "0.0.0.0", 443)
	inbound := resource("inbound", "stream", "0.0.0.0", 443)
	front.Capabilities.RouteHints = []string{"shares-listener:fronting-owner"}
	inbound.Capabilities.RouteHints = []string{"shares-listener:fronting-owner"}
	if got := DetectCollisions([]hostresources.ProtectableResource{front, inbound}); len(got) != 0 {
		t.Fatalf("explicit public 443 fronting ownership collided: %#v", got)
	}
}
