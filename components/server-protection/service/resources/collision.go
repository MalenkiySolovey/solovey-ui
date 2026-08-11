package resources

import (
	"sort"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type CollisionSeverity string

const (
	CollisionWarning CollisionSeverity = "warning"
	CollisionError   CollisionSeverity = "error"
)

type Collision struct {
	Code            string            `json:"code"`
	Severity        CollisionSeverity `json:"severity"`
	LeftResourceID  string            `json:"leftResourceId"`
	RightResourceID string            `json:"rightResourceId"`
	Protocol        string            `json:"protocol"`
	Port            int               `json:"port"`
	Message         string            `json:"message"`
}

func DetectCollisions(items []hostresources.ProtectableResource) []Collision {
	result := make([]Collision, 0)
	for leftIndex := 0; leftIndex < len(items); leftIndex++ {
		left := items[leftIndex]
		if left.Port < 1 || left.Port > 65535 {
			continue
		}
		for rightIndex := leftIndex + 1; rightIndex < len(items); rightIndex++ {
			right := items[rightIndex]
			if right.Port != left.Port || socketProtocol(right.Protocol) != socketProtocol(left.Protocol) {
				continue
			}
			if documentedSharedListener(left, right) {
				continue
			}
			if collision, ok := compareListeners(left, right); ok {
				result = append(result, collision)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].LeftResourceID + "\x00" + result[i].RightResourceID + "\x00" + result[i].Code
		right := result[j].LeftResourceID + "\x00" + result[j].RightResourceID + "\x00" + result[j].Code
		return left < right
	})
	return result
}

func compareListeners(left, right hostresources.ProtectableResource) (Collision, bool) {
	leftListen := hostresources.NormalizeListen(left.Listen)
	rightListen := hostresources.NormalizeListen(right.Listen)
	base := Collision{
		LeftResourceID:  left.ID,
		RightResourceID: right.ID,
		Protocol:        socketProtocol(left.Protocol),
		Port:            left.Port,
	}
	if leftListen.Value == rightListen.Value {
		base.Code = "listener_collision"
		base.Severity = CollisionError
		base.Message = "resources claim the same listener and port"
		return base, true
	}
	if ipv4IPv6WildcardPair(leftListen, rightListen) {
		base.Code = "dual_stack_ambiguous"
		base.Severity = CollisionWarning
		base.Message = "IPv4 and IPv6 wildcard ownership may overlap on this host"
		return base, true
	}
	if wildcardConflicts(leftListen, rightListen) {
		base.Code = "wildcard_listener_collision"
		base.Severity = CollisionError
		base.Message = "a wildcard listener overlaps another resource on the same port"
		return base, true
	}
	if leftListen.Class == hostresources.ListenHostname || rightListen.Class == hostresources.ListenHostname {
		base.Code = "hostname_listener_ambiguous"
		base.Severity = CollisionWarning
		base.Message = "hostname listener ownership cannot be proven without runtime resolution"
		return base, true
	}
	return Collision{}, false
}

func socketProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "udp":
		return "udp"
	default:
		return "tcp"
	}
}

func wildcardConflicts(left, right hostresources.NormalizedListen) bool {
	if left.Class == hostresources.ListenWildcard || right.Class == hostresources.ListenWildcard {
		return true
	}
	if left.Class == hostresources.ListenIPv4Wildcard {
		return right.Family == 4 || right.Class == hostresources.ListenHostname
	}
	if right.Class == hostresources.ListenIPv4Wildcard {
		return left.Family == 4 || left.Class == hostresources.ListenHostname
	}
	if left.Class == hostresources.ListenIPv6Wildcard {
		return right.Family == 6 || right.Class == hostresources.ListenHostname
	}
	if right.Class == hostresources.ListenIPv6Wildcard {
		return left.Family == 6 || left.Class == hostresources.ListenHostname
	}
	return false
}

func ipv4IPv6WildcardPair(left, right hostresources.NormalizedListen) bool {
	return (left.Class == hostresources.ListenIPv4Wildcard && right.Class == hostresources.ListenIPv6Wildcard) ||
		(left.Class == hostresources.ListenIPv6Wildcard && right.Class == hostresources.ListenIPv4Wildcard)
}

func documentedSharedListener(left, right hostresources.ProtectableResource) bool {
	leftHints := sharedListenerHints(left.Capabilities.RouteHints)
	for _, hint := range right.Capabilities.RouteHints {
		if _, ok := leftHints[hint]; ok && strings.HasPrefix(hint, "shares-listener:") {
			return true
		}
	}
	return false
}

func sharedListenerHints(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "shares-listener:") {
			result[value] = struct{}{}
		}
	}
	return result
}
