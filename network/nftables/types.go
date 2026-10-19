// Package nftables implements the closed local network platform adapter.
package nftables

import "bytes"

const (
	MaxManagedCandidateBytes = 256 << 10
	MaxOutputBytes           = 512 << 10
	MaxArtifactBytes         = 512 << 10
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

type Correlation struct {
	OperationID  string `json:"operation_id"`
	InstanceID   string `json:"instance_id"`
	LockRevision int    `json:"lock_revision"`
}

type NFTValidateRequest struct {
	CandidateCapturedAtUnixNano   int64  `json:"candidate_captured_at_unix_nano,omitempty"`
	CandidatePath                 string `json:"candidate_path"`
	ExpectedRevision              string `json:"expected_revision"`
	ExpectedSHA256                string `json:"expected_sha256"`
	ExpectedSemanticSHA256        string `json:"expected_semantic_sha256"`
	ExpectedTimedMembershipSHA256 string `json:"expected_timed_membership_sha256"`
}

type NFTObserveRequest struct{}

type NFTApplyRequest struct {
	CandidateCapturedAtUnixNano           int64  `json:"candidate_captured_at_unix_nano,omitempty"`
	CandidatePath                         string `json:"candidate_path"`
	RollbackArtifactPath                  string `json:"rollback_artifact_path"`
	ExpectedTable                         string `json:"expected_table"`
	ExpectedRevision                      string `json:"expected_revision"`
	ExpectedSHA256                        string `json:"expected_sha256"`
	ExpectedPreviousRevision              string `json:"expected_previous_revision,omitempty"`
	ExpectedPreviousTablePresent          bool   `json:"expected_previous_table_present"`
	ExpectedSemanticSHA256                string `json:"expected_semantic_sha256"`
	ExpectedPreviousSemanticSHA256        string `json:"expected_previous_semantic_sha256,omitempty"`
	ExpectedTimedMembershipSHA256         string `json:"expected_timed_membership_sha256"`
	ExpectedPreviousTimedMembershipSHA256 string `json:"expected_previous_timed_membership_sha256,omitempty"`
}

type NFTRollbackRequest struct {
	RollbackArtifactPath                 string `json:"rollback_artifact_path"`
	ExpectedTable                        string `json:"expected_table"`
	ExpectedSHA256                       string `json:"expected_sha256"`
	ExpectedCurrentRevision              string `json:"expected_current_revision"`
	ExpectedCurrentSemanticSHA256        string `json:"expected_current_semantic_sha256"`
	ExpectedCurrentTimedMembershipSHA256 string `json:"expected_current_timed_membership_sha256"`
}

type NFTResult struct {
	ManagedTablePresent           bool   `json:"managed_table_present"`
	AppliedRevision               string `json:"applied_revision,omitempty"`
	CandidateSHA256               string `json:"candidate_sha256,omitempty"`
	RollbackSHA256                string `json:"rollback_sha256,omitempty"`
	PreviousRevision              string `json:"previous_revision,omitempty"`
	PreviousTablePresent          bool   `json:"previous_table_present"`
	SemanticSHA256                string `json:"semantic_sha256,omitempty"`
	PreviousSemanticSHA256        string `json:"previous_semantic_sha256,omitempty"`
	CurrentRevision               string `json:"current_revision,omitempty"`
	CurrentSemanticSHA256         string `json:"current_semantic_sha256,omitempty"`
	TimedMembershipSHA256         string `json:"timed_membership_sha256,omitempty"`
	PreviousTimedMembershipSHA256 string `json:"previous_timed_membership_sha256,omitempty"`
	CurrentTimedMembershipSHA256  string `json:"current_timed_membership_sha256,omitempty"`
}

type ManagedRoot interface {
	Path() string
	Resolve(string, bool) (string, error)
}
