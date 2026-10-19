package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
)

// Compatibility with the released typed broker wire contract. Components own
// no nft executor, renderer, transaction or cleanup implementation here.
type Correlation = nftables.Correlation
type NFTValidateRequest = nftables.NFTValidateRequest
type NFTObserveRequest = nftables.NFTObserveRequest
type NFTApplyRequest = nftables.NFTApplyRequest
type NFTRollbackRequest = nftables.NFTRollbackRequest
type NFTResult = nftables.NFTResult
type NFTSupport = nftables.NFTSupport
type NFTExecutor = nftables.NFTExecutor

const ManagedNFTSemanticSchemaV1 = nftables.ManagedNFTSemanticSchemaV1
const ManagedNFTTimedMembershipSchemaV1 = nftables.ManagedNFTTimedMembershipSchemaV1

func ManagedSemanticSHA256(data []byte) (string, error) { return nftables.ManagedSemanticSHA256(data) }
func ManagedTimedMembershipSHA256(data []byte) (string, error) {
	return nftables.ManagedTimedMembershipSHA256(data)
}
func RemoveManagedTableForPackageRemoval(ctx context.Context) error {
	return nftables.RemoveManagedTableForPackageRemoval(ctx)
}

// The helper also uses this ordinary artifact digest for nginx/negotiation.
func sha256Hex(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
