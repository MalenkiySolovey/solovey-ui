// Package sshposture binds integration fixtures to the existing SSH owner's
// exact value types and pure projections. It grants no SSH mutation capability.
package sshposture

import (
	"time"

	"github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	owner "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type (
	AuthenticationPostureV1 = owner.AuthenticationPostureV1
	AuthorizedKeysPostureV1 = owner.AuthorizedKeysPostureV1
	BinaryIdentityV1        = owner.BinaryIdentityV1
	CapabilitySetV1         = owner.CapabilitySetV1
	ConfigNodeV1            = owner.ConfigNodeV1
	ForwardingPostureV1     = owner.ForwardingPostureV1
	HostKeyPostureV1        = owner.HostKeyPostureV1
	MatchContextV1          = owner.MatchContextV1
	SSHListenerAuthorityV1  = owner.SSHListenerAuthorityV1
	SSHPostureV1            = owner.SSHPostureV1
	ServiceIdentityV1       = owner.ServiceIdentityV1
)

const (
	AvailabilityAvailable        = owner.AvailabilityAvailable
	ListenerAuthoritySchemaV1    = owner.ListenerAuthoritySchemaV1
	MaxListenerAuthorityLifetime = owner.MaxListenerAuthorityLifetime
	MaxPostureLifetime           = owner.MaxPostureLifetime
	PostureSchemaV1              = owner.PostureSchemaV1
	ProtectionResourceKind       = owner.ProtectionResourceKind
	ProtectionResourceOwner      = owner.ProtectionResourceOwner
)

func Revision(value any) string { return owner.Revision(value) }
func PostureSemanticRevision(posture SSHPostureV1) string {
	return owner.PostureSemanticRevision(posture)
}
func ProtectableResources(posture SSHPostureV1, now time.Time) ([]resources.ProtectableResource, error) {
	return owner.ProtectableResources(posture, now)
}
