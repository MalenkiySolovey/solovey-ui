// Package nftables implements the closed local network platform adapter.
package nftables

import canonical "github.com/MalenkiySolovey/solovey-host/network/nftcontract"

import "bytes"

const (
	MaxManagedCandidateBytes = canonical.MaxManagedCandidateBytes
	MaxOutputBytes           = canonical.MaxOutputBytes
	MaxArtifactBytes         = canonical.MaxArtifactBytes
)

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || original > 0
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return original, nil
}

type Correlation = canonical.Correlation

type NFTValidateRequest = canonical.NFTValidateRequest

type NFTObserveRequest = canonical.NFTObserveRequest

type NFTApplyRequest = canonical.NFTApplyRequest

type NFTRollbackRequest = canonical.NFTRollbackRequest

type NFTResult = canonical.NFTResult

type ManagedRoot interface {
	Path() string
	Resolve(string, bool) (string, error)
}
