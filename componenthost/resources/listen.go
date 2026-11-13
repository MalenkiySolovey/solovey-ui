package resources

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"

type ListenClass = canonical.ListenClass
type NormalizedListen = canonical.NormalizedListen

const (
	ListenWildcard     = canonical.ListenWildcard
	ListenIPv4Wildcard = canonical.ListenIPv4Wildcard
	ListenIPv6Wildcard = canonical.ListenIPv6Wildcard
	ListenLoopback     = canonical.ListenLoopback
	ListenPublicExact  = canonical.ListenPublicExact
	ListenHostname     = canonical.ListenHostname
)

func NormalizeListen(value string) NormalizedListen { return canonical.NormalizeListen(value) }
