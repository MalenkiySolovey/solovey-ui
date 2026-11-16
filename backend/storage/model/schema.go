package model

// SchemaModels returns the existing Panel durable record set. It opens no database
// and includes no optional Component tables; their existing migration owners compose them.
func SchemaModels() []any {
	return []any{
		&Setting{}, &Tls{}, &TLSCertificateProvider{}, &Inbound{}, &Outbound{},
		&Service{}, &Endpoint{}, &User{}, &Tokens{},
		&Stats{}, &ClientIP{}, &Client{}, &Changes{},
		&AuditEvent{}, &FailoverMemberState{}, &InboundDraft{},
		&ComponentMigration{}, &InboundFallbackCheckpoint{}, &InboundEndpointLease{},
		&AdminMFAFactor{}, &AdminRecoveryCode{}, &SecuritySession{}, &StepUpGrant{},
		&SSHPostureSnapshot{}, &SSHManagementCandidate{}, &SSHManagedArtifactCheckpoint{},
		&SSHReconnectChallenge{}, &SSHRecoveryEvidence{}, &SSHManagementJournal{},
		&DeploymentState{}, &DeploymentOperation{}, &DeploymentJournal{}, &DeploymentDoctorSnapshot{},
		&UpdateReleaseState{}, &UpdateOperation{}, &UpdateJournal{},
		&ResourcePressureState{}, &ResourcePressureTransition{}, &MigrationJournal{},
		&DataLifecycleOperation{}, &DataLifecycleJournal{},
	}
}
