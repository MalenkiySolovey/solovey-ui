//go:build !minimal

package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

const (
	nodeArtifactSchema       = "solovey-ui/fallback-html-node-artifact/v1"
	nodePublishPlanSchema    = "solovey-ui/fallback-html-node-publish-plan/v1"
	nodeCapabilityPublicSite = "public-site-runtime"
	nodeCapabilityVersion    = "v1"
	nodeSignatureMode        = "orchestrator-channel"
)

type NodeCapabilityRequirement struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Runtimes []string `json:"runtimes"`
}

type NodeEndpointContract struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type NodeSignatureContract struct {
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}

type NodeApplyContract struct {
	StagingRequired   bool `json:"stagingRequired"`
	AtomicSwap        bool `json:"atomicSwap"`
	RollbackOnFailure bool `json:"rollbackOnFailure"`
}

type NodeArtifactContract struct {
	Schema               string                      `json:"schema"`
	SiteArtifactSchema   string                      `json:"siteArtifactSchema"`
	Version              string                      `json:"version"`
	CreatedAt            int64                       `json:"createdAt"`
	RequiredCapabilities []NodeCapabilityRequirement `json:"requiredCapabilities"`
	Endpoints            []NodeEndpointContract      `json:"endpoints"`
	Signature            NodeSignatureContract       `json:"signature"`
	Apply                NodeApplyContract           `json:"apply"`
	Files                []NodeArtifactFile          `json:"files"`
	Safety               SafetyReport                `json:"safety"`
}

type NodeArtifactFile struct {
	PublicPath string `json:"publicPath"`
	Relative   string `json:"relative"`
	Sha256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
}

type NodePublishPlan struct {
	Schema               string                      `json:"schema"`
	SiteID               uint                        `json:"siteId"`
	NodeID               string                      `json:"nodeId,omitempty"`
	Version              string                      `json:"version"`
	Artifact             NodeArtifactRef             `json:"artifact"`
	RequiredCapabilities []NodeCapabilityRequirement `json:"requiredCapabilities"`
	Endpoints            []NodeEndpointContract      `json:"endpoints"`
	Signature            NodeSignatureContract       `json:"signature"`
	Apply                NodeApplyContract           `json:"apply"`
	Status               NodePublicationView         `json:"status"`
}

type NodeArtifactRef struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Sha256      string `json:"sha256"`
	SizeBytes   int    `json:"sizeBytes"`
}

type NodePublicationView struct {
	ID             uint   `json:"id,omitempty"`
	SiteID         uint   `json:"siteId"`
	NodeID         string `json:"nodeId,omitempty"`
	PublishVersion string `json:"publishVersion"`
	Runtime        string `json:"runtime"`
	Status         string `json:"status"`
	ArtifactSha256 string `json:"artifactSha256,omitempty"`
	OperationID    string `json:"operationId,omitempty"`
	LastError      string `json:"lastError,omitempty"`
	CreatedAt      int64  `json:"createdAt,omitempty"`
	UpdatedAt      int64  `json:"updatedAt,omitempty"`
	AppliedAt      int64  `json:"appliedAt,omitempty"`
}

func (s *Service) GetNodePublishPlan(siteID uint, version string, nodeID string) (NodePublishPlan, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return NodePublishPlan{}, errors.New("publish artifact version is required")
	}
	archive, err := s.GetPublishArtifact(siteID, version)
	if err != nil {
		return NodePublishPlan{}, err
	}
	status, err := s.nodePublicationStatus(siteID, version, nodeID)
	if err != nil {
		return NodePublishPlan{}, err
	}
	return NodePublishPlan{
		Schema:               nodePublishPlanSchema,
		SiteID:               siteID,
		NodeID:               strings.TrimSpace(nodeID),
		Version:              version,
		Artifact:             NodeArtifactRef{Filename: archive.Filename, ContentType: archive.ContentType, Sha256: archive.Sha256, SizeBytes: archive.SizeBytes},
		RequiredCapabilities: nodeCapabilityRequirements(),
		Endpoints:            nodeEndpointContracts(),
		Signature:            nodeSignatureContract(),
		Apply:                nodeApplyContract(),
		Status:               status,
	}, nil
}

func (s *Service) ListNodePublications(siteID uint) ([]NodePublicationView, error) {
	var rows []fallbackdomain.NodePublication
	if err := s.db.Where("site_id = ?", siteID).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]NodePublicationView, 0, len(rows))
	for _, row := range rows {
		out = append(out, nodePublicationView(row))
	}
	return out, nil
}

func (s *Service) nodePublicationStatus(siteID uint, version string, nodeID string) (NodePublicationView, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodePublicationView{
			SiteID:         siteID,
			PublishVersion: version,
			Runtime:        "unknown",
			Status:         "not-targeted",
		}, nil
	}
	var row fallbackdomain.NodePublication
	err := s.db.
		Where("site_id = ? AND node_id = ? AND publish_version = ?", siteID, nodeID, version).
		Order("updated_at DESC, id DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NodePublicationView{
			SiteID:         siteID,
			NodeID:         nodeID,
			PublishVersion: version,
			Runtime:        "unknown",
			Status:         "not-targeted",
		}, nil
	}
	if err != nil {
		return NodePublicationView{}, err
	}
	return nodePublicationView(row), nil
}

func nodePublicationView(row fallbackdomain.NodePublication) NodePublicationView {
	return NodePublicationView{
		ID:             row.ID,
		SiteID:         row.SiteID,
		NodeID:         row.NodeID,
		PublishVersion: row.PublishVersion,
		Runtime:        row.Runtime,
		Status:         row.Status,
		ArtifactSha256: row.ArtifactSha256,
		OperationID:    row.OperationID,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		AppliedAt:      row.AppliedAt,
	}
}

func buildNodeArtifactContract(version string, createdAt int64, files []publishArtifactFile, report SafetyReport) NodeArtifactContract {
	nodeFiles := make([]NodeArtifactFile, 0, len(files))
	for _, file := range files {
		nodeFiles = append(nodeFiles, NodeArtifactFile{
			PublicPath: file.PublicPath,
			Relative:   file.Relative,
			Sha256:     file.Sha256,
			SizeBytes:  file.SizeBytes,
		})
	}
	return NodeArtifactContract{
		Schema:               nodeArtifactSchema,
		SiteArtifactSchema:   "solovey-ui/fallback-html-site/v1",
		Version:              version,
		CreatedAt:            createdAt,
		RequiredCapabilities: nodeCapabilityRequirements(),
		Endpoints:            nodeEndpointContracts(),
		Signature:            nodeSignatureContract(),
		Apply:                nodeApplyContract(),
		Files:                nodeFiles,
		Safety:               report,
	}
}

func nodeCapabilityRequirements() []NodeCapabilityRequirement {
	return []NodeCapabilityRequirement{{
		ID:       nodeCapabilityPublicSite,
		Version:  nodeCapabilityVersion,
		Runtimes: []string{"gin", "nginx", "caddy"},
	}}
}

func nodeEndpointContracts() []NodeEndpointContract {
	return []NodeEndpointContract{
		{Method: "GET", Path: "/capabilities"},
		{Method: "POST", Path: "/public-site/validate"},
		{Method: "POST", Path: "/public-site/apply"},
		{Method: "POST", Path: "/public-site/rollback"},
		{Method: "GET", Path: "/public-site/status/{siteId}"},
		{Method: "GET", Path: "/public-site/health/{siteId}"},
		{Method: "GET", Path: "/public-site/content/{siteId}/{path...}"},
	}
}

func nodeSignatureContract() NodeSignatureContract {
	return NodeSignatureContract{Mode: nodeSignatureMode, Required: true}
}

func nodeApplyContract() NodeApplyContract {
	return NodeApplyContract{StagingRequired: true, AtomicSwap: true, RollbackOnFailure: true}
}

func archiveDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateNodePublicationStatus(value string) (string, error) {
	status := strings.TrimSpace(strings.ToLower(value))
	switch status {
	case "planned", "validating", "applying", "active", "failed", "rollback", "cleanup-pending", "removed":
		return status, nil
	default:
		return "", fmt.Errorf("unsupported node publication status %q", value)
	}
}

func newNodePublication(siteID uint, version string, nodeID string, artifactSha string, runtime string, status string) (fallbackdomain.NodePublication, error) {
	status, err := validateNodePublicationStatus(status)
	if err != nil {
		return fallbackdomain.NodePublication{}, err
	}
	now := time.Now().Unix()
	return fallbackdomain.NodePublication{
		SiteID:         siteID,
		NodeID:         strings.TrimSpace(nodeID),
		PublishVersion: strings.TrimSpace(version),
		Runtime:        strings.TrimSpace(runtime),
		Status:         status,
		ArtifactSha256: strings.TrimSpace(artifactSha),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}
