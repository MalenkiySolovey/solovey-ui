// Code generated from github.com/MalenkiySolovey/solovey-contracts@v1.0.0; DO NOT EDIT.
// Source commit: ca13e3bb8e516594467dcbf717f757d4806d553c
// Deprecated: import github.com/MalenkiySolovey/solovey-contracts/management/v1 directly.
package managementv1

import canonical "github.com/MalenkiySolovey/solovey-contracts/management/v1"

const Version = canonical.Version
const MaxDocumentBytes = canonical.MaxDocumentBytes

type ActionDescriptor = canonical.ActionDescriptor
type ArtifactIdentity = canonical.ArtifactIdentity
type ArtifactTrust = canonical.ArtifactTrust
type Capability = canonical.Capability
type Change = canonical.Change
type Command = canonical.Command
type CommandResult = canonical.CommandResult
type Component = canonical.Component
type ComponentManifest = canonical.ComponentManifest
type ComponentRemoveInput = canonical.ComponentRemoveInput
type Digest = canonical.Digest
type Generation = canonical.Generation
type Health = canonical.Health
type HealthEvidence = canonical.HealthEvidence
type HostRef = canonical.HostRef
type Identifier = canonical.Identifier
type Job = canonical.Job
type ManagementGrant = canonical.ManagementGrant
type NetworkIntentRef = canonical.NetworkIntentRef
type NetworkObservation = canonical.NetworkObservation
type NodeID = canonical.NodeID
type NodeIdentity = canonical.NodeIdentity
type OperationRef = canonical.OperationRef
type Outcome = canonical.Outcome
type Problem = canonical.Problem
type Progress = canonical.Progress
type ProofKind = canonical.ProofKind
type ReasonCode = canonical.ReasonCode
type ResourceRef = canonical.ResourceRef
type RestoreInput = canonical.RestoreInput
type Revision = canonical.Revision
type SecretRef = canonical.SecretRef

func LookupAction(id string) (ActionDescriptor, bool)   { return canonical.LookupAction(id) }
func KnownReason(reason ReasonCode) bool                { return canonical.KnownReason(reason) }
func EffectiveReason(reason ReasonCode) ReasonCode      { return canonical.EffectiveReason(reason) }
func Accepted(result CommandResult) bool                { return canonical.Accepted(result) }
func Decode(name string, data []byte, target any) error { return canonical.Decode(name, data, target) }
func ValidateDocument(name string, data []byte) error   { return canonical.ValidateDocument(name, data) }
func DecodeCommand(data []byte) (Command, error)        { return canonical.DecodeCommand(data) }
func ValidateCommand(command Command) error             { return canonical.ValidateCommand(command) }
func OwnerInputDigest(schema, input string) string      { return canonical.OwnerInputDigest(schema, input) }
func CommandDigest(command Command) (string, error)     { return canonical.CommandDigest(command) }
