//go:build !minimal

package service

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	nodeArtifactSchema       = "solovey-ui/fallback-html-node-artifact/v1"
	nodeCapabilityPublicSite = "public-site-runtime"
	nodeCapabilityVersion    = "v1"
	nodeSignatureMode        = "orchestrator-channel"
)

type nodeCapabilityRequirement struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Runtimes []string `json:"runtimes"`
}

type nodeEndpointContract struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type nodeSignatureSpec struct {
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}

type nodeApplyContractSpec struct {
	StagingRequired   bool `json:"stagingRequired"`
	AtomicSwap        bool `json:"atomicSwap"`
	RollbackOnFailure bool `json:"rollbackOnFailure"`
}

type nodeArtifactContract struct {
	Schema               string                      `json:"schema"`
	SiteArtifactSchema   string                      `json:"siteArtifactSchema"`
	Version              string                      `json:"version"`
	CreatedAt            int64                       `json:"createdAt"`
	RequiredCapabilities []nodeCapabilityRequirement `json:"requiredCapabilities"`
	Endpoints            []nodeEndpointContract      `json:"endpoints"`
	Signature            nodeSignatureSpec           `json:"signature"`
	Apply                nodeApplyContractSpec       `json:"apply"`
	Files                []nodeArtifactFile          `json:"files"`
	Safety               SafetyReport                `json:"safety"`
}

type nodeArtifactFile struct {
	PublicPath string `json:"publicPath"`
	Relative   string `json:"relative"`
	Sha256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
}

func buildNodeArtifactContract(version string, createdAt int64, files []publishArtifactFile, report SafetyReport) nodeArtifactContract {
	nodeFiles := make([]nodeArtifactFile, 0, len(files))
	for _, file := range files {
		nodeFiles = append(nodeFiles, nodeArtifactFile{
			PublicPath: file.PublicPath,
			Relative:   file.Relative,
			Sha256:     file.Sha256,
			SizeBytes:  file.SizeBytes,
		})
	}
	return nodeArtifactContract{
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

func nodeCapabilityRequirements() []nodeCapabilityRequirement {
	return []nodeCapabilityRequirement{{
		ID:       nodeCapabilityPublicSite,
		Version:  nodeCapabilityVersion,
		Runtimes: []string{"gin", "nginx", "caddy"},
	}}
}

func nodeEndpointContracts() []nodeEndpointContract {
	return []nodeEndpointContract{
		{Method: "GET", Path: "/capabilities"},
		{Method: "POST", Path: "/public-site/validate"},
		{Method: "POST", Path: "/public-site/apply"},
		{Method: "POST", Path: "/public-site/rollback"},
		{Method: "GET", Path: "/public-site/status/{siteId}"},
		{Method: "GET", Path: "/public-site/health/{siteId}"},
		{Method: "GET", Path: "/public-site/content/{siteId}/{path...}"},
	}
}

func nodeSignatureContract() nodeSignatureSpec {
	return nodeSignatureSpec{Mode: nodeSignatureMode, Required: true}
}

func nodeApplyContract() nodeApplyContractSpec {
	return nodeApplyContractSpec{StagingRequired: true, AtomicSwap: true, RollbackOnFailure: true}
}

func archiveDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
