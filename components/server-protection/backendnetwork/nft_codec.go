package backendnetwork

import (
	"time"

	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
)

type NFTCodec struct{}

func (NFTCodec) RenderProtection(intent networkintent.ProtectionEnforcementIntent, managed bool, at time.Time) string {
	return nftables.RenderProtection(intent, managed, at)
}
func (NFTCodec) CheckProtectionCandidate(data []byte, revision, digest string) error {
	return nftables.CheckProtectionCandidate(data, revision, digest)
}
func (NFTCodec) ManagedSemanticSHA256(data []byte) (string, error) {
	return nftables.ManagedSemanticSHA256(data)
}
func (NFTCodec) ManagedTimedMembershipSHA256(data []byte) (string, error) {
	return nftables.ManagedTimedMembershipSHA256(data)
}
func (NFTCodec) DeleteOwnedTableCandidate() []byte { return nftables.DeleteOwnedTableCandidate() }
