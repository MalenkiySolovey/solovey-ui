//go:build !minimal

package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	nodeHTTPTimeout       = 30 * time.Second
	nodeMaxResponseBytes  = 1 << 20
	nodeDefaultRuntime    = "gin"
	nodeDirectStatusReady = "active"
)

type NodeClient interface {
	Validate(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error)
	Apply(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error)
}

type NodeApplyInput struct {
	NodeID       string `json:"nodeId"`
	BaseURL      string `json:"baseUrl"`
	Runtime      string `json:"runtime"`
	SharedSecret string `json:"sharedSecret,omitempty"`
}

type NodeEndpointInput struct {
	NodeID       string `json:"nodeId"`
	BaseURL      string `json:"baseUrl"`
	Runtime      string `json:"runtime"`
	SharedSecret string `json:"sharedSecret,omitempty"`
	Enabled      *bool  `json:"enabled"`
}

type NodeEndpointView struct {
	ID              uint   `json:"id"`
	NodeID          string `json:"nodeId"`
	BaseURL         string `json:"baseUrl"`
	Runtime         string `json:"runtime"`
	HasSharedSecret bool   `json:"hasSharedSecret"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type NodeApplyTarget struct {
	NodeID       string
	BaseURL      string
	SiteID       uint
	Runtime      string
	OperationID  string
	SharedSecret string
}

type NodeApplyResult struct {
	Plan       NodePublishPlan     `json:"plan"`
	Validation NodeRuntimeStatus   `json:"validation"`
	Status     NodePublicationView `json:"status"`
}

type NodeRuntimeStatus struct {
	OK              bool   `json:"ok,omitempty"`
	SiteID          string `json:"siteId,omitempty"`
	Version         string `json:"version,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	Status          string `json:"status,omitempty"`
	ArtifactSha256  string `json:"artifactSha256,omitempty"`
	PreviousVersion string `json:"previousVersion,omitempty"`
	LastError       string `json:"lastError,omitempty"`
	AppliedAt       int64  `json:"appliedAt,omitempty"`
	UpdatedAt       int64  `json:"updatedAt,omitempty"`
	Files           int    `json:"files,omitempty"`
}

type HTTPNodeClient struct {
	client            *http.Client
	allowInsecureHTTP bool
	skipURLValidation bool
}

func NewHTTPNodeClient(client *http.Client) *HTTPNodeClient {
	if client == nil {
		client = ssrf.NewHTTPClient(nodeHTTPTimeout, "https")
	}
	return &HTTPNodeClient{client: client}
}

func (s *Service) SetNodeClient(client NodeClient) {
	if client == nil {
		client = NewHTTPNodeClient(nil)
	}
	s.nodeClient = client
}

func (h *HTTPNodeClient) Validate(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
	return h.postArtifact(ctx, target, artifact, "/public-site/validate")
}

func (h *HTTPNodeClient) Apply(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
	return h.postArtifact(ctx, target, artifact, "/public-site/apply")
}

func (h *HTTPNodeClient) postArtifact(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive, endpoint string) (NodeRuntimeStatus, error) {
	baseURL, err := h.normalizeBaseURL(target.BaseURL)
	if err != nil {
		return NodeRuntimeStatus{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+endpoint, bytes.NewReader(artifact.Data))
	if err != nil {
		return NodeRuntimeStatus{}, err
	}
	request.Header.Set("Content-Type", artifact.ContentType)
	request.Header.Set("X-Solovey-Site-ID", fmt.Sprint(target.SiteID))
	request.Header.Set("X-Solovey-Runtime", target.Runtime)
	request.Header.Set("X-Solovey-Artifact-Sha256", artifact.Sha256)
	signNodeRequest(request, target, artifact.Data)
	response, err := h.client.Do(request)
	if err != nil {
		return NodeRuntimeStatus{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, nodeMaxResponseBytes+1))
	if err != nil {
		return NodeRuntimeStatus{}, err
	}
	if len(body) > nodeMaxResponseBytes {
		return NodeRuntimeStatus{}, errors.New("node response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return NodeRuntimeStatus{}, fmt.Errorf("node %s failed with status %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var status NodeRuntimeStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return NodeRuntimeStatus{}, err
	}
	return status, nil
}

func (h *HTTPNodeClient) normalizeBaseURL(raw string) (string, error) {
	return normalizeNodeBaseURL(raw, h.allowInsecureHTTP, !h.skipURLValidation)
}

func normalizeNodeBaseURL(raw string, allowInsecureHTTP bool, validateOutbound bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("node base URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.User != nil {
		return "", errors.New("node base URL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("node base URL must not contain query or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("node base URL must not contain a path")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && !(allowInsecureHTTP && scheme == "http") {
		return "", errors.New("node base URL must use https")
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "", "", "", ""
	if validateOutbound {
		if err := ssrf.ValidateOutboundURL(context.Background(), parsed.String(), scheme); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (s *Service) ApplyPublishToNode(ctx context.Context, siteID uint, version string, input NodeApplyInput, actor string) (NodeApplyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := s.resolveNodeApplyTarget(siteID, input)
	if err != nil {
		return NodeApplyResult{}, err
	}
	archive, err := s.GetPublishArtifact(siteID, version)
	if err != nil {
		return NodeApplyResult{}, err
	}
	plan, err := s.GetNodePublishPlan(siteID, version, target.NodeID)
	if err != nil {
		return NodeApplyResult{}, err
	}
	operationID := newNodeOperationID()
	target.OperationID = operationID
	if err := s.upsertNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, "validating", operationID, "", 0); err != nil {
		return NodeApplyResult{}, err
	}
	validation, err := s.nodeClient.Validate(ctx, target, archive)
	if err != nil {
		_ = s.failNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, operationID, err)
		return NodeApplyResult{}, err
	}
	if err := s.upsertNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, "applying", operationID, "", 0); err != nil {
		return NodeApplyResult{}, err
	}
	runtimeStatus, err := s.nodeClient.Apply(ctx, target, archive)
	if err != nil {
		_ = s.failNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, operationID, err)
		return NodeApplyResult{}, err
	}
	appliedAt := runtimeStatus.AppliedAt
	if appliedAt == 0 {
		appliedAt = time.Now().Unix()
	}
	if runtimeStatus.ArtifactSha256 != "" && runtimeStatus.ArtifactSha256 != archive.Sha256 {
		err := fmt.Errorf("node applied unexpected artifact sha256 %s", runtimeStatus.ArtifactSha256)
		_ = s.failNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, operationID, err)
		return NodeApplyResult{}, err
	}
	if err := s.upsertNodePublication(siteID, version, target.NodeID, archive.Sha256, target.Runtime, nodeDirectStatusReady, operationID, runtimeStatus.LastError, appliedAt); err != nil {
		return NodeApplyResult{}, err
	}
	if err := recordEvent(s.db, siteID, actor, "node_publish_applied", map[string]any{
		"nodeId":      target.NodeID,
		"version":     version,
		"runtime":     target.Runtime,
		"operationId": operationID,
	}); err != nil {
		return NodeApplyResult{}, err
	}
	status, err := s.nodePublicationStatus(siteID, version, target.NodeID)
	if err != nil {
		return NodeApplyResult{}, err
	}
	plan.Status = status
	return NodeApplyResult{Plan: plan, Validation: validation, Status: status}, nil
}

func (s *Service) ListNodeEndpoints() ([]NodeEndpointView, error) {
	var rows []fallbackdomain.NodeEndpoint
	if err := s.db.Order("node_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]NodeEndpointView, 0, len(rows))
	for _, row := range rows {
		out = append(out, nodeEndpointView(row))
	}
	return out, nil
}

func (s *Service) SaveNodeEndpoint(input NodeEndpointInput, actor string) (NodeEndpointView, error) {
	nodeID, err := normalizeNodeID(input.NodeID)
	if err != nil {
		return NodeEndpointView{}, err
	}
	baseURL, err := normalizeNodeBaseURL(input.BaseURL, false, false)
	if err != nil {
		return NodeEndpointView{}, err
	}
	runtime, err := normalizeNodeRuntime(input.Runtime, nodeDefaultRuntime)
	if err != nil {
		return NodeEndpointView{}, err
	}
	sharedSecret := strings.TrimSpace(input.SharedSecret)
	var existing fallbackdomain.NodeEndpoint
	if err := s.db.Where("node_id = ?", nodeID).First(&existing).Error; err == nil && sharedSecret == "" {
		sharedSecret = existing.SharedSecret
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return NodeEndpointView{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().Unix()
	row := fallbackdomain.NodeEndpoint{
		NodeID:       nodeID,
		BaseURL:      baseURL,
		Runtime:      runtime,
		SharedSecret: sharedSecret,
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Select("NodeID", "BaseURL", "Runtime", "SharedSecret", "Enabled", "CreatedAt", "UpdatedAt").Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"base_url":      row.BaseURL,
			"runtime":       row.Runtime,
			"shared_secret": row.SharedSecret,
			"enabled":       row.Enabled,
			"updated_at":    row.UpdatedAt,
		}),
	}).Create(&row).Error; err != nil {
		return NodeEndpointView{}, err
	}
	if err := s.db.Where("node_id = ?", nodeID).First(&row).Error; err != nil {
		return NodeEndpointView{}, err
	}
	if err := recordEvent(s.db, 0, actor, "node_endpoint_saved", map[string]any{"nodeId": nodeID, "runtime": runtime}); err != nil {
		return NodeEndpointView{}, err
	}
	return nodeEndpointView(row), nil
}

func (s *Service) DeleteNodeEndpoint(nodeID string, actor string) error {
	nodeID, err := normalizeNodeID(nodeID)
	if err != nil {
		return err
	}
	if err := s.db.Where("node_id = ?", nodeID).Delete(&fallbackdomain.NodeEndpoint{}).Error; err != nil {
		return err
	}
	return recordEvent(s.db, 0, actor, "node_endpoint_deleted", map[string]any{"nodeId": nodeID})
}

func (s *Service) resolveNodeApplyTarget(siteID uint, input NodeApplyInput) (NodeApplyTarget, error) {
	target, err := normalizeNodeApplyTarget(siteID, input)
	if err != nil {
		return NodeApplyTarget{}, err
	}
	if target.BaseURL != "" {
		return target, nil
	}
	var endpoint fallbackdomain.NodeEndpoint
	err = s.db.Where("node_id = ? AND enabled = ?", target.NodeID, true).First(&endpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NodeApplyTarget{}, errors.New("node endpoint is not registered or disabled")
	}
	if err != nil {
		return NodeApplyTarget{}, err
	}
	target.BaseURL = endpoint.BaseURL
	target.SharedSecret = endpoint.SharedSecret
	if strings.TrimSpace(input.Runtime) == "" {
		target.Runtime = endpoint.Runtime
	}
	return target, nil
}

func normalizeNodeApplyTarget(siteID uint, input NodeApplyInput) (NodeApplyTarget, error) {
	nodeID := strings.TrimSpace(input.NodeID)
	nodeID, err := normalizeNodeID(nodeID)
	if err != nil {
		return NodeApplyTarget{}, err
	}
	runtime, err := normalizeNodeRuntime(input.Runtime, nodeDefaultRuntime)
	if err != nil {
		return NodeApplyTarget{}, err
	}
	return NodeApplyTarget{
		NodeID:       nodeID,
		BaseURL:      strings.TrimSpace(input.BaseURL),
		SiteID:       siteID,
		Runtime:      runtime,
		SharedSecret: strings.TrimSpace(input.SharedSecret),
	}, nil
}

func normalizeNodeID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("node id is required")
	}
	if len(value) > 128 {
		return "", errors.New("node id is too long")
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return "", errors.New("node id may contain only letters, digits, dots, underscores and dashes")
		}
	}
	return value, nil
}

func normalizeNodeRuntime(value string, fallback string) (string, error) {
	runtime := strings.TrimSpace(strings.ToLower(value))
	if runtime == "" {
		runtime = fallback
	}
	switch runtime {
	case "gin", "nginx", "caddy":
		return runtime, nil
	default:
		return "", fmt.Errorf("unsupported node runtime %q", value)
	}
}

func nodeEndpointView(row fallbackdomain.NodeEndpoint) NodeEndpointView {
	return NodeEndpointView{
		ID:              row.ID,
		NodeID:          row.NodeID,
		BaseURL:         row.BaseURL,
		Runtime:         row.Runtime,
		HasSharedSecret: strings.TrimSpace(row.SharedSecret) != "",
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func signNodeRequest(request *http.Request, target NodeApplyTarget, body []byte) {
	secret := strings.TrimSpace(target.SharedSecret)
	if secret == "" || request == nil || request.URL == nil {
		return
	}
	operationID := strings.TrimSpace(target.OperationID)
	if operationID == "" {
		operationID = newNodeOperationID()
	}
	nonce := newNodeOperationID()
	timestamp := fmt.Sprint(time.Now().Unix())
	sum := sha256.Sum256(body)
	bodySha := hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonicalNodeSignaturePayload(request.Method, request.URL.Path, timestamp, nonce, operationID, bodySha)))
	request.Header.Set("X-Solovey-Operation-ID", operationID)
	request.Header.Set("X-Solovey-Nonce", nonce)
	request.Header.Set("X-Solovey-Timestamp", timestamp)
	request.Header.Set("X-Solovey-Body-Sha256", bodySha)
	request.Header.Set("X-Solovey-Signature", hex.EncodeToString(mac.Sum(nil)))
}

func canonicalNodeSignaturePayload(method string, requestPath string, timestamp string, nonce string, operationID string, bodySha string) string {
	return strings.ToUpper(method) + "\n" + requestPath + "\n" + timestamp + "\n" + nonce + "\n" + operationID + "\n" + bodySha
}

func (s *Service) upsertNodePublication(siteID uint, version string, nodeID string, artifactSha string, runtime string, status string, operationID string, lastError string, appliedAt int64) error {
	status, err := validateNodePublicationStatus(status)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	row := fallbackdomain.NodePublication{
		SiteID:         siteID,
		NodeID:         strings.TrimSpace(nodeID),
		PublishVersion: strings.TrimSpace(version),
		Runtime:        strings.TrimSpace(runtime),
		Status:         status,
		ArtifactSha256: strings.TrimSpace(artifactSha),
		OperationID:    strings.TrimSpace(operationID),
		LastError:      strings.TrimSpace(lastError),
		CreatedAt:      now,
		UpdatedAt:      now,
		AppliedAt:      appliedAt,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "site_id"}, {Name: "node_id"}, {Name: "publish_version"}},
		DoUpdates: clause.Assignments(map[string]any{
			"runtime":         row.Runtime,
			"status":          row.Status,
			"artifact_sha256": row.ArtifactSha256,
			"operation_id":    row.OperationID,
			"last_error":      row.LastError,
			"updated_at":      row.UpdatedAt,
			"applied_at":      row.AppliedAt,
		}),
	}).Create(&row).Error
}

func (s *Service) failNodePublication(siteID uint, version string, nodeID string, artifactSha string, runtime string, operationID string, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	return s.upsertNodePublication(siteID, version, nodeID, artifactSha, runtime, "failed", operationID, msg, 0)
}

func newNodeOperationID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "node-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("node-%d", time.Now().UnixNano())
}
