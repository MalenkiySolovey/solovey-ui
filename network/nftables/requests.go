package nftables

import "errors"

func (r NFTValidateRequest) Validate(root ManagedRoot) error {
	if !validRevision(r.ExpectedRevision) || !validSHA256(r.ExpectedSHA256) || !validSHA256(r.ExpectedSemanticSHA256) || !validSHA256(r.ExpectedTimedMembershipSHA256) {
		return errors.New("nft validation requires bounded candidate artifact and semantic identities")
	}
	_, err := root.Resolve(r.CandidatePath, true)
	return err
}

func (r NFTApplyRequest) Validate(root ManagedRoot) error {
	if r.ExpectedTable != managedTable {
		return errors.New("nft apply is restricted to inet solovey_protection")
	}
	if _, err := root.Resolve(r.CandidatePath, true); err != nil {
		return err
	}
	if !validRevision(r.ExpectedRevision) || !validSHA256(r.ExpectedSHA256) || !validSHA256(r.ExpectedSemanticSHA256) || !validSHA256(r.ExpectedTimedMembershipSHA256) {
		return errors.New("nft apply requires bounded candidate artifact and semantic identities")
	}
	if r.ExpectedPreviousTablePresent {
		if !validRevision(r.ExpectedPreviousRevision) || !validSHA256(r.ExpectedPreviousSemanticSHA256) || !validSHA256(r.ExpectedPreviousTimedMembershipSHA256) {
			return errors.New("nft apply previous managed-table fence is malformed")
		}
	} else if r.ExpectedPreviousRevision != "" || r.ExpectedPreviousSemanticSHA256 != "" || r.ExpectedPreviousTimedMembershipSHA256 != "" {
		return errors.New("nft apply absent-table fence contains a previous identity")
	}
	_, err := root.Resolve(r.RollbackArtifactPath, false)
	return err
}

func (r NFTRollbackRequest) Validate(root ManagedRoot) error {
	if r.ExpectedTable != managedTable {
		return errors.New("nft rollback is restricted to inet solovey_protection")
	}
	if r.ExpectedSHA256 != "" && !validSHA256(r.ExpectedSHA256) || !validSHA256(r.ExpectedCurrentSemanticSHA256) || !validSHA256(r.ExpectedCurrentTimedMembershipSHA256) {
		return errors.New("nft rollback SHA-256 is malformed")
	}
	if !validRevision(r.ExpectedCurrentRevision) {
		return errors.New("nft rollback current revision fence is malformed")
	}
	_, err := root.Resolve(r.RollbackArtifactPath, false)
	return err
}
