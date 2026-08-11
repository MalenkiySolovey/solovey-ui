//go:build !minimal

package fallbackhtml

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"gorm.io/gorm"
)

type targetProvider struct {
	db                    *gorm.DB
	runtime               *fallbackservice.Runtime
	now                   func() time.Time
	tlsServerNameVerified func(string) bool
}

const publishedRedirectPolicyRevision = "fallback-html-published-redirect-safety-v1"

type publishedRedirectFact struct {
	FromDigest string `json:"fromDigest"`
	ToDigest   string `json:"toDigest"`
	StatusCode int    `json:"statusCode"`
	Class      string `json:"class"`
}

func (targetProvider) ProviderID() string { return id }

func (p targetProvider) ListTargets(ctx context.Context) ([]neutral.TargetV1, error) {
	if p.db == nil {
		return nil, fmt.Errorf("fallback-html database is unavailable")
	}
	var sites []fallbackdomain.Site
	if err := p.db.WithContext(ctx).
		Preload("Targets", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Preload("Publishes", func(tx *gorm.DB) *gorm.DB { return tx.Where("active = ?", true).Order("id ASC") }).
		Preload("Publishes.Files", func(tx *gorm.DB) *gorm.DB { return tx.Order("public_path ASC, id ASC") }).
		Where("enabled = ? AND status = ?", true, "published").Order("id ASC").Find(&sites).Error; err != nil {
		return nil, err
	}
	now := time.Now
	if p.now != nil {
		now = p.now
	}
	observed := now().UTC()
	status := fallbackservice.RuntimeStatus{}
	health := fallbackservice.RuntimeHealth{}
	if p.runtime != nil {
		status = p.runtime.Status()
		health = fallbackservice.New(p.db, p.runtime).RuntimeHealth()
	}
	result := make([]neutral.TargetV1, 0, len(sites))
	for _, site := range sites {
		if len(site.Publishes) == 0 {
			continue
		}
		publish := site.Publishes[0]
		ambiguous := len(site.Publishes) != 1
		target, warnings, err := siteResourceTarget(site)
		if err != nil {
			return nil, err
		}
		normalized := hostresources.NormalizeListen(target.Listen)
		port := uint16(0)
		if target.Port > 0 && target.Port <= 65535 {
			port = uint16(target.Port)
		}
		local := normalized.Class == hostresources.ListenLoopback
		ready := neutral.ReadinessNotReady
		reasons := []string(nil)
		if len(warnings) > 0 {
			reasons = append(reasons, "fallback_target_legacy_selection")
			ambiguous = true
		}
		if len(site.Publishes) > 1 {
			reasons = append(reasons, "fallback_target_publish_ambiguous")
		}
		management := hostresources.CapabilityYes
		if strings.EqualFold(target.Kind, "standalone") && local {
			management = hostresources.CapabilityNo
			if p.runtime != nil && status.Active && health.OK && status.SiteID == site.ID && p.runtime.Owns(normalized.Value, target.Port) {
				ready = neutral.ReadinessReady
			} else {
				reasons = append(reasons, "fallback_target_runtime_not_active")
			}
		} else {
			reasons = append(reasons, "fallback_target_not_isolated_local_endpoint")
		}
		confidence := 10000
		if ambiguous {
			ready = neutral.ReadinessUnknown
			confidence = 0
			reasons = append(reasons, "fallback_target_ambiguous")
		}
		contentDigest := publishDigest(publish)
		healthRevision := hostresources.Revision(struct {
			Active                  bool
			Healthy                 bool
			SiteID                  uint
			Pages, Redirects        int
			PublishRevision, Listen string
			Port                    int
		}{status.Active, health.OK, status.SiteID, status.Pages, status.Redirects, publish.Version, normalized.Value, target.Port})
		result = append(result, neutral.TargetV1{Schema: neutral.TargetSchemaV1, Identity: neutral.TargetIdentity{ProviderID: id, TargetID: "site:" + strconv.FormatUint(uint64(site.ID), 10)}, PublishRevision: publish.Version, ContentDigest: contentDigest, Endpoint: neutral.EndpointCapability{EndpointID: "fallback-html:endpoint:" + strconv.FormatUint(uint64(site.ID), 10) + ":" + strconv.FormatUint(uint64(target.ID), 10), Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyForListen(target.Listen), Bind: normalized.Value, Port: port, TLS: capability(target.TLS), Local: local, CanReachManagement: management}, Readiness: ready, ProviderHealthRevision: healthRevision, ObservedAt: observed.Unix(), ExpiresAt: observed.Add(90 * time.Second).Unix(), Source: "fallback-html:published-runtime", ConfidenceBP: confidence, ReasonCodes: reasons})
	}
	return result, nil
}

func publishDigest(publish fallbackdomain.Publish) string {
	type fileFact struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	files := make([]fileFact, 0, len(publish.Files))
	for _, file := range publish.Files {
		files = append(files, fileFact{file.PublicPath, file.Sha256, file.SizeBytes})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	payload, _ := json.Marshal(struct {
		Version string     `json:"version"`
		Files   []fileFact `json:"files"`
	}{publish.Version, files})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func publishDigestV2(publish fallbackdomain.Publish) string {
	redirects, _, _ := publishedRedirectFacts(publish)
	payload, _ := json.Marshal(struct {
		PublishDigest  string                  `json:"publishDigest"`
		RedirectPolicy string                  `json:"redirectPolicy"`
		Redirects      []publishedRedirectFact `json:"redirects"`
	}{publishDigest(publish), publishedRedirectPolicyRevision, redirects})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func publishedRedirectFacts(publish fallbackdomain.Publish) ([]publishedRedirectFact, hostresources.CapabilityValue, string) {
	facts := make([]publishedRedirectFact, 0, len(publish.Redirects))
	reason := ""
	for _, redirect := range publish.Redirects {
		class, currentReason := classifyPublishedRedirect(redirect)
		facts = append(facts, publishedRedirectFact{
			FromDigest: digestPublishedRedirectValue(redirect.FromPath),
			ToDigest:   digestPublishedRedirectValue(redirect.ToPath),
			StatusCode: redirect.StatusCode,
			Class:      class,
		})
		if currentReason == "redirect_target_invalid" || reason == "" {
			reason = currentReason
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].FromDigest != facts[j].FromDigest {
			return facts[i].FromDigest < facts[j].FromDigest
		}
		if facts[i].ToDigest != facts[j].ToDigest {
			return facts[i].ToDigest < facts[j].ToDigest
		}
		if facts[i].StatusCode != facts[j].StatusCode {
			return facts[i].StatusCode < facts[j].StatusCode
		}
		return facts[i].Class < facts[j].Class
	})
	if reason != "" {
		return facts, hostresources.CapabilityUnknown, reason
	}
	return facts, hostresources.CapabilityNo, ""
}

func classifyPublishedRedirect(redirect fallbackdomain.PublishRedirect) (string, string) {
	fromPath, err := fallbackdomain.ValidatePagePath(redirect.FromPath, nil)
	if err != nil || fromPath != redirect.FromPath {
		return "unknown", "redirect_target_invalid"
	}
	statusCode, err := fallbackdomain.ValidateRedirectStatus(redirect.StatusCode)
	if err != nil || statusCode != redirect.StatusCode {
		return "unknown", "redirect_target_invalid"
	}
	toPath, external, err := fallbackdomain.NormalizeRedirectTarget(redirect.ToPath, nil)
	if err != nil || toPath != redirect.ToPath || external != redirect.External {
		return "unknown", "redirect_target_invalid"
	}
	if external {
		return "external_absolute", "external_redirect_unverified"
	}
	if fromPath == toPath {
		return "unknown", "redirect_target_invalid"
	}
	return "provider_local_relative", ""
}

func digestPublishedRedirectValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func capability(value bool) hostresources.CapabilityValue {
	if value {
		return hostresources.CapabilityYes
	}
	return hostresources.CapabilityNo
}
