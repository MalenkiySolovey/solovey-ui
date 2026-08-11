package artifacts

import (
	"errors"
	"os"
)

const (
	frontingStateName = "fronting-state.json"
	firewallStateName = "firewall-state.json"
)

// WriteFrontingState publishes the typed restart checkpoint below an existing
// operation directory. The filename is fixed and cannot be supplied by API/UI.
func (s *Storage) WriteFrontingState(operationID string, data []byte) error {
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 256<<10 {
		return errors.New("fronting checkpoint must be bounded")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.atomicWrite(filepathSlash("operations", operationID, frontingStateName), data)
}

func (s *Storage) ReadFrontingState(operationID string) ([]byte, error) {
	if err := validateOperationID(operationID); err != nil {
		return nil, err
	}
	path, err := s.resolve(filepathSlash("operations", operationID, frontingStateName), true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 256<<10 {
		return nil, errors.New("fronting checkpoint is invalid")
	}
	return data, nil
}

// WriteFirewallState publishes the exact managed-table rollback checkpoint at
// a fixed path. API and UI data cannot select the filename or escape the
// operation directory.
func (s *Storage) WriteFirewallState(operationID string, data []byte) error {
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 64<<10 {
		return errors.New("firewall checkpoint must be bounded")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.atomicWrite(filepathSlash("operations", operationID, firewallStateName), data)
}

func (s *Storage) ReadFirewallState(operationID string) ([]byte, error) {
	if err := validateOperationID(operationID); err != nil {
		return nil, err
	}
	path, err := s.resolve(filepathSlash("operations", operationID, firewallStateName), true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 64<<10 {
		return nil, errors.New("firewall checkpoint is invalid")
	}
	return data, nil
}

// ReadFirewallRollbackEvidence reads only the helper-owned, fixed rollback
// artifact and sidecar and proves that they agree before returning the digest.
func (s *Storage) ReadFirewallRollbackEvidence(revision string) (string, error) {
	if err := validateSegment(revision); err != nil {
		return "", err
	}
	rollbackPath, err := s.resolve(filepathSlash("revisions", revision, "firewall-before.nft"), true)
	if err != nil {
		return "", err
	}
	rollback, err := os.ReadFile(rollbackPath)
	if err != nil {
		return "", err
	}
	if len(rollback) == 0 || len(rollback) > 512<<10 {
		return "", errors.New("firewall rollback artifact is invalid")
	}
	shaPath, err := s.resolve(filepathSlash("revisions", revision, "firewall-before.nft.sha256"), true)
	if err != nil {
		return "", err
	}
	recorded, err := os.ReadFile(shaPath)
	if err != nil {
		return "", err
	}
	expected := string(recorded)
	for len(expected) > 0 && (expected[len(expected)-1] == '\n' || expected[len(expected)-1] == '\r') {
		expected = expected[:len(expected)-1]
	}
	if safeSHA256(expected) != expected || digestArtifact(rollback) != expected {
		return "", ErrChecksumMismatch
	}
	return expected, nil
}
