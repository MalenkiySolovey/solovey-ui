//go:build !minimal

package service

import feature "github.com/MalenkiySolovey/solovey-component-fallback-html/service"

// Compatibility names delegate to the immutable component source.
type AssetView = feature.AssetView
type ExternalResourceView = feature.ExternalResourceView
type Content = feature.Content
type ContentChange = feature.ContentChange
type PublicationDraft = feature.PublicationDraft
type ArtifactPart = feature.ArtifactPart

const ArtifactSite = feature.ArtifactSite
const ArtifactTargets = feature.ArtifactTargets
const ArtifactRoutes = feature.ArtifactRoutes
const ArtifactSafety = feature.ArtifactSafety
const ArtifactNode = feature.ArtifactNode
const ArtifactChecksums = feature.ArtifactChecksums

type TemplateFile = feature.TemplateFile
type RuntimeHealth = feature.RuntimeHealth
type HostFacts = feature.HostFacts
type HTTPHostFacts = feature.HTTPHostFacts
type HTTPBindingScope = feature.HTTPBindingScope
type PublicationBinding = feature.PublicationBinding
type HTTPServing = feature.HTTPServing
type SiteImportInput = feature.SiteImportInput
type SiteImportPage = feature.SiteImportPage
type SiteImportRedirect = feature.SiteImportRedirect
type SiteImportResult = feature.SiteImportResult

const LegacySelfStealSource = feature.LegacySelfStealSource
const LegacySelfStealRetiredReason = feature.LegacySelfStealRetiredReason
const LegacySelfStealInvalidReason = feature.LegacySelfStealInvalidReason
const LegacySelfStealRetiredStatus = feature.LegacySelfStealRetiredStatus
const LegacySelfStealInvalidStatus = feature.LegacySelfStealInvalidStatus

type LegacySelfStealInspection = feature.LegacySelfStealInspection

var DecodeLegacySelfStealPayload = feature.DecodeLegacySelfStealPayload

type PublishResult = feature.PublishResult
type PublishView = feature.PublishView
type ArtifactArchive = feature.ArtifactArchive
type RollbackInput = feature.RollbackInput
type PrunePublishesInput = feature.PrunePublishesInput
type PrunePublishesResult = feature.PrunePublishesResult
type SafetyReport = feature.SafetyReport
type RemoteTemplateCatalog = feature.RemoteTemplateCatalog
type RemoteTemplateView = feature.RemoteTemplateView
type RuntimeStatus = feature.RuntimeStatus

var ErrRuntimeDraining = feature.ErrRuntimeDraining
var RuntimeTargetKey = feature.RuntimeTargetKey

type PublicationSource = feature.PublicationSource
type PublicationCandidate = feature.PublicationCandidate
type PublicHandler = feature.PublicHandler
type PublicSurface = feature.PublicSurface
type RequestLimitDecision = feature.RequestLimitDecision
type RequestLimiter = feature.RequestLimiter
type SiteInput = feature.SiteInput
type PageInput = feature.PageInput
type PathValidationInput = feature.PathValidationInput
type PathValidationResult = feature.PathValidationResult
type RedirectInput = feature.RedirectInput
type ExternalResourceInput = feature.ExternalResourceInput
type PreviewInput = feature.PreviewInput
type PreviewResult = feature.PreviewResult

var RecordEvent = feature.RecordEvent
var ErrRecordNotFound = feature.ErrRecordNotFound

type Store = feature.Store
type TargetObservationRequest = feature.TargetObservationRequest
type TargetObservation = feature.TargetObservation
type TargetObserver = feature.TargetObserver
type PublicationState = feature.PublicationState

var ConfiguredTargetIdentity = feature.ConfiguredTargetIdentity

type TargetInput = feature.TargetInput
type TargetView = feature.TargetView
type PortCandidate = feature.PortCandidate
type RuntimeOption = feature.RuntimeOption

var SafeArchiveName = feature.SafeArchiveName

var portCandidateKey = feature.PortCandidateKey
