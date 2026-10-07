package tracker

import "strings"

const (
	TrackerValidatedSingBoxModule  = "github.com/sagernet/sing-box"
	TrackerValidatedSingBoxVersion = "v1.14.2"
	TrackerRevalidationPolicyName  = "sing-box tracker revalidation policy"
)

var TrackerRevalidationChecks = []string{
	"RoutedConnection signature still matches sing-box adapter.ConnectionTracker",
	"RoutedPacketConnection signature still matches sing-box adapter.ConnectionTracker",
	"wrapped TCP connections always call Done exactly once on Close or terminal I/O error",
	"wrapped packet connections always call Done exactly once on Close or terminal I/O error",
	"Reset fences the old connection generation before closing and draining its wrappers",
	"StatsTracker keeps counter pointers stable across Reset for already wrapped connections",
	"source IP extraction from adapter.InboundContext still uses metadata.Source.Addr",
	"atomic IP admission is invoked exactly once before tracking each TCP or packet connection",
	"old wrappers cannot update counters or remove connections in a new core generation",
	"runtime health and stats projections belong to the current core generation",
	"one official actual-flow inventory projects each accepted TCP, packet or L3 flow once",
	"rejected TCP, packet and L3 flows never enter the accepted inventory",
}

type TrackerRevalidationStatus struct {
	Module           string
	ValidatedVersion string
	CurrentVersion   string
	Required         bool
	PolicyName       string
	Checks           []string
}

func SingBoxTrackerRevalidationStatus(currentVersion string) TrackerRevalidationStatus {
	currentVersion = normalizeTrackerVersion(currentVersion)
	validatedVersion := normalizeTrackerVersion(TrackerValidatedSingBoxVersion)
	return TrackerRevalidationStatus{Module: TrackerValidatedSingBoxModule, ValidatedVersion: validatedVersion,
		CurrentVersion: currentVersion, Required: currentVersion == "" || currentVersion != validatedVersion,
		PolicyName: TrackerRevalidationPolicyName, Checks: append([]string(nil), TrackerRevalidationChecks...)}
}

func normalizeTrackerVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}
