//go:build !minimal

package fallbackhtml

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

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
