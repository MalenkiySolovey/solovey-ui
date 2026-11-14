package firewall

import (
	"time"

	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
)

// NFTCodec consumes typed intent and bounded candidate bytes. It owns syntax
// and canonical artifact identity; this port cannot observe or execute a rule.
type NFTCodec interface {
	RenderProtection(networkintent.ProtectionEnforcementIntent, bool, time.Time) string
	CheckProtectionCandidate([]byte, string, string) error
	ManagedSemanticSHA256([]byte) (string, error)
	ManagedTimedMembershipSHA256([]byte) (string, error)
	DeleteOwnedTableCandidate() []byte
}
