package hostsurface

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
)

const ListenerOwnerFactSchemaV1 = canonical.ListenerOwnerFactSchemaV1

const MaxListenerOwnerFactLifetime = canonical.MaxListenerOwnerFactLifetime

const ListenerProofPIDFDSocketV1 = canonical.ListenerProofPIDFDSocketV1

const ListenerProofProcFSV1 = canonical.ListenerProofProcFSV1

type ListenerSocketIdentityV1 = canonical.ListenerSocketIdentityV1

type ListenerApplicationIdentityV1 = canonical.ListenerApplicationIdentityV1

type ListenerOwnerFactV1 = canonical.ListenerOwnerFactV1

func ValidListenerSocketIdentity(value ListenerSocketIdentityV1) bool {
	return canonical.ValidListenerSocketIdentity(value)
}

func ListenerSocketIdentityMatchesSurface(value ListenerSocketIdentityV1, inode string, cookie uint64) bool {
	return canonical.ListenerSocketIdentityMatchesSurface(value, inode, cookie)
}

func OwnerObservationSetRevision(facts []HostSurfaceFactV1, reasons []string) string {
	return canonical.OwnerObservationSetRevision(facts, reasons)
}

func validOwnerSocket(value ListenerSocketIdentityV1) bool {
	return canonical.ValidListenerSocketIdentity(value)
}
