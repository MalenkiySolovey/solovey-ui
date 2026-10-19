package nftables

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Engine is invoked only behind the existing authenticated typed broker. It
// owns scoped candidate validation, atomic replacement and verified rollback.
// ManagedRoot retains its deployment owner's path/authority rechecks.
type Engine struct {
	root     ManagedRoot
	executor NFTExecutor
	now      func() time.Time
}

func NewEngine(root ManagedRoot) Engine {
	return NewEngineWithExecutor(root, newSystemNFTExecutor(), time.Now)
}

// NewEngineWithExecutor composes deterministic fixture/platform implementations.
// The system executor itself is private; callers can only use validated Engine
// operations against the fixed managed table.
func NewEngineWithExecutor(root ManagedRoot, executor NFTExecutor, now func() time.Time) Engine {
	return Engine{root: root, executor: executor, now: now}
}

func (e Engine) Support(ctx context.Context) NFTSupport {
	if e.executor == nil || ctx == nil || ctx.Err() != nil {
		return NFTSupport{Reason: "nft_capability_unknown"}
	}
	return e.executor.Detect(ctx)
}

var shaPattern = regexp.MustCompile("^[a-f0-9]{64}$")
var revisionPattern = regexp.MustCompile("^[a-f0-9]{16,128}$")

func validSHA256(value string) bool   { return shaPattern.MatchString(value) }
func validRevision(value string) bool { return revisionPattern.MatchString(value) }

func (e Engine) Validate(ctx context.Context, correlation Correlation, request NFTValidateRequest) (*NFTResult, error) {
	if err := e.ready(ctx, correlation); err != nil {
		return nil, err
	}
	if err := request.Validate(e.root); err != nil {
		return nil, err
	}
	path, err := e.root.Resolve(request.CandidatePath, true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateCandidate(data, request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	data, err = materializeManagedNFTCandidate(data, request.CandidateCapturedAtUnixNano, e.currentTime())
	if err != nil {
		return nil, err
	}
	candidateSemantic, err := managedSemanticSHA(data)
	if err != nil {
		return nil, err
	}
	if request.ExpectedSemanticSHA256 != "" && request.ExpectedSemanticSHA256 != candidateSemantic {
		return nil, errors.New("candidate semantic identity mismatch")
	}
	candidateMembership, err := managedTimedMembershipSHA(data)
	if err != nil {
		return nil, err
	}
	if request.ExpectedTimedMembershipSHA256 != candidateMembership {
		return nil, errors.New("candidate timed membership identity mismatch")
	}
	currentState, err := observeManagedTable(ctx, e.executor)
	if err != nil {
		return nil, err
	}
	present := currentState.present
	previousRevision := ""
	previousSemantic := ""
	if present {
		previousRevision = currentState.revision
		previousSemantic = currentState.semanticSHA
		if previousRevision == "" || previousSemantic == "" {
			return nil, errors.New("current managed table has no proven Solovey owner")
		}
	}
	transaction := append([]byte(nil), data...)
	if present {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/validate-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	return &NFTResult{CandidateSHA256: request.ExpectedSHA256, SemanticSHA256: candidateSemantic, TimedMembershipSHA256: candidateMembership,
		PreviousRevision: previousRevision, PreviousSemanticSHA256: previousSemantic, PreviousTimedMembershipSHA256: currentState.timedMembershipSHA, PreviousTablePresent: present}, nil
}

func (e Engine) Observe(ctx context.Context) (*NFTResult, error) {
	if ctx == nil || ctx.Err() != nil || e.executor == nil {
		return nil, errors.New("network adapter observation unavailable")
	}
	state, err := observeManagedTable(ctx, e.executor)
	if err != nil {
		return nil, err
	}
	return &NFTResult{ManagedTablePresent: state.present, CurrentRevision: state.revision, CurrentSemanticSHA256: state.semanticSHA, CurrentTimedMembershipSHA256: state.timedMembershipSHA}, nil
}

func (e Engine) Apply(ctx context.Context, correlation Correlation, request NFTApplyRequest) (*NFTResult, error) {
	if err := e.ready(ctx, correlation); err != nil {
		return nil, err
	}
	if err := request.Validate(e.root); err != nil {
		return nil, err
	}
	candidatePath, err := e.root.Resolve(request.CandidatePath, true)
	if err != nil {
		return nil, err
	}
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return nil, err
	}
	if err := validateCandidate(candidate, request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	candidate, err = materializeManagedNFTCandidate(candidate, request.CandidateCapturedAtUnixNano, e.currentTime())
	if err != nil {
		return nil, err
	}
	candidateSemantic, err := managedSemanticSHA(candidate)
	if err != nil {
		return nil, err
	}
	if request.ExpectedSemanticSHA256 != "" && request.ExpectedSemanticSHA256 != candidateSemantic {
		return nil, errors.New("candidate semantic identity mismatch")
	}
	candidateMembership, err := managedTimedMembershipSHA(candidate)
	if err != nil {
		return nil, err
	}
	if request.ExpectedTimedMembershipSHA256 != candidateMembership {
		return nil, errors.New("candidate timed membership identity mismatch")
	}
	beforeState, err := observeManagedTable(ctx, e.executor)
	if err != nil {
		return nil, err
	}
	before, present := beforeState.raw, beforeState.present
	if present != request.ExpectedPreviousTablePresent {
		return nil, errors.New("managed table presence changed after validation")
	}
	rollback := []byte("delete table inet solovey_protection\n")
	previousRevision := ""
	if present {
		previousRevision = beforeState.revision
		if previousRevision == "" || beforeState.semanticSHA == "" {
			return nil, errors.New("current managed table is not safely restorable")
		}
		if previousRevision != request.ExpectedPreviousRevision || beforeState.semanticSHA != request.ExpectedPreviousSemanticSHA256 || beforeState.timedMembershipSHA != request.ExpectedPreviousTimedMembershipSHA256 {
			return nil, errors.New("managed table identity changed after validation")
		}
		rollback, err = captureManagedNFTRollback(before, e.currentTime())
		if err != nil {
			return nil, err
		}
		if len(rollback) == 0 || rollback[len(rollback)-1] != '\n' {
			rollback = append(rollback, '\n')
		}
	}
	if _, err := writeManagedAtomic(e.root, request.RollbackArtifactPath, rollback); err != nil {
		return nil, err
	}
	rollbackSHA := sha256Hex(rollback)
	if _, err := writeManagedAtomic(e.root, request.RollbackArtifactPath+".sha256", []byte(rollbackSHA+"\n")); err != nil {
		return nil, err
	}
	transaction := append([]byte(nil), candidate...)
	if present {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	if err := validateManagedScope(transaction, true); err != nil {
		return nil, err
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/apply-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	if err := e.executor.ApplyManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	afterState, err := observeManagedTable(ctx, e.executor)
	afterPresent := afterState.present
	if err != nil || !afterPresent {
		return nil, errors.Join(errors.New("managed table is absent after apply"), err)
	}
	after := afterState.raw
	if err := verifyManagedRevision(after, request.ExpectedRevision); err != nil {
		return nil, errors.New("managed table revision verification failed")
	}
	if request.ExpectedSemanticSHA256 != "" && afterState.semanticSHA != request.ExpectedSemanticSHA256 {
		return nil, errors.New("managed table semantic identity verification failed")
	}
	if afterState.timedMembershipSHA != request.ExpectedTimedMembershipSHA256 {
		return nil, errors.New("managed table timed membership verification failed")
	}
	previousSemantic := ""
	if present {
		previousSemantic = beforeState.semanticSHA
	}
	return &NFTResult{ManagedTablePresent: true, AppliedRevision: request.ExpectedRevision, CandidateSHA256: request.ExpectedSHA256, SemanticSHA256: candidateSemantic, TimedMembershipSHA256: candidateMembership,
		RollbackSHA256: rollbackSHA, PreviousRevision: previousRevision, PreviousSemanticSHA256: previousSemantic, PreviousTimedMembershipSHA256: beforeState.timedMembershipSHA, PreviousTablePresent: present}, nil
}

func (e Engine) Rollback(ctx context.Context, correlation Correlation, request NFTRollbackRequest) (*NFTResult, error) {
	if err := e.ready(ctx, correlation); err != nil {
		return nil, err
	}
	if err := request.Validate(e.root); err != nil {
		return nil, err
	}
	path, err := e.root.Resolve(request.RollbackArtifactPath, true)
	if err != nil {
		return nil, err
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	shaPath, err := e.root.Resolve(request.RollbackArtifactPath+".sha256", true)
	if err != nil {
		return nil, err
	}
	recordedSHA, err := os.ReadFile(shaPath)
	if err != nil {
		return nil, err
	}
	expectedSHA := strings.TrimSpace(string(recordedSHA))
	if !validSHA256(expectedSHA) || (request.ExpectedSHA256 != "" && request.ExpectedSHA256 != expectedSHA) || sha256Hex(artifact) != expectedSHA {
		return nil, errors.New("rollback artifact SHA-256 mismatch")
	}
	materialized, _, err := materializeManagedNFTRollback(artifact, e.currentTime())
	if err != nil {
		return nil, err
	}
	if err := validateManagedScope(materialized, true); err != nil {
		return nil, err
	}
	currentState, err := observeManagedTable(ctx, e.executor)
	if err != nil {
		return nil, err
	}
	_, present := currentState.raw, currentState.present
	desiredAbsent := strings.TrimSpace(string(materialized)) == "delete table inet solovey_protection"
	expectedRevision := ""
	expectedSemantic := ""
	expectedMembership := ""
	if !desiredAbsent {
		expectedRevision, err = managedRevision(materialized)
		if err != nil {
			return nil, fmt.Errorf("rollback artifact has no restorable managed revision: %w", err)
		}
		expectedSemantic, err = managedSemanticSHA(materialized)
		if err != nil {
			return nil, fmt.Errorf("rollback artifact has no restorable semantic identity: %w", err)
		}
		expectedMembership, err = managedTimedMembershipSHA(materialized)
		if err != nil {
			return nil, fmt.Errorf("rollback artifact has no restorable timed membership identity: %w", err)
		}
	}
	if !present {
		if desiredAbsent {
			return &NFTResult{ManagedTablePresent: false, RollbackSHA256: expectedSHA}, nil
		}
		return nil, errors.New("managed table is absent before rollback")
	}
	currentRevision := currentState.revision
	if currentRevision == "" || currentState.semanticSHA == "" {
		return nil, errors.New("current managed table is not safely replaceable during rollback")
	}
	if !desiredAbsent && currentRevision == expectedRevision && currentState.semanticSHA == expectedSemantic && currentState.timedMembershipSHA == expectedMembership {
		return &NFTResult{ManagedTablePresent: true, RollbackSHA256: expectedSHA, SemanticSHA256: expectedSemantic, TimedMembershipSHA256: expectedMembership,
			CurrentRevision: currentRevision, CurrentSemanticSHA256: currentState.semanticSHA, CurrentTimedMembershipSHA256: currentState.timedMembershipSHA}, nil
	}
	if currentRevision != request.ExpectedCurrentRevision || currentState.semanticSHA != request.ExpectedCurrentSemanticSHA256 || currentState.timedMembershipSHA != request.ExpectedCurrentTimedMembershipSHA256 {
		return nil, errors.New("managed table current revision does not match rollback fence")
	}
	transaction := append([]byte(nil), materialized...)
	if present && !desiredAbsent {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/rollback-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	if err := e.executor.ApplyManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	afterState, err := observeManagedTable(ctx, e.executor)
	after, afterPresent := afterState.raw, afterState.present
	if err != nil || afterPresent == desiredAbsent {
		return nil, errors.Join(errors.New("managed table rollback verification failed"), err)
	}
	if afterPresent {
		if err := verifyManagedRevision(after, expectedRevision); err != nil {
			return nil, errors.New("managed table rollback revision verification failed")
		}
		if expectedSemantic != "" && afterState.semanticSHA != expectedSemantic {
			return nil, errors.New("managed table rollback semantic identity verification failed")
		}
		if afterState.timedMembershipSHA != expectedMembership {
			return nil, errors.New("managed table rollback timed membership verification failed")
		}
	}
	return &NFTResult{ManagedTablePresent: afterPresent, RollbackSHA256: expectedSHA, SemanticSHA256: expectedSemantic, TimedMembershipSHA256: expectedMembership,
		CurrentRevision: afterState.revision, CurrentSemanticSHA256: afterState.semanticSHA, CurrentTimedMembershipSHA256: afterState.timedMembershipSHA}, nil
}

func (e Engine) currentTime() time.Time {
	if e.now != nil {
		return e.now().UTC()
	}
	return time.Now().UTC()
}

var operationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func (e Engine) ready(ctx context.Context, correlation Correlation) error {
	if ctx == nil || ctx.Err() != nil || e.root == nil || e.executor == nil || !operationPattern.MatchString(correlation.OperationID) {
		return errors.New("network adapter operation unavailable")
	}
	return nil
}
