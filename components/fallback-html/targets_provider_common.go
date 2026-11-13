//go:build !minimal

package fallbackhtml

import (
	feature "github.com/MalenkiySolovey/solovey-component-fallback-html/targets"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"gorm.io/gorm"
	"time"
)

type targetProvider struct {
	db                    *gorm.DB
	runtime               *fallbackservice.Runtime
	observer              fallbackservice.TargetObserver
	now                   func() time.Time
	tlsServerNameVerified func(string) bool
}

func (targetProvider) ProviderID() string { return id }

const publishedRedirectPolicyRevision = feature.PublishedRedirectPolicyRevision

type publishedRedirectFact = feature.PublishedRedirectFact

var publishDigest = feature.PublishDigest
var publishDigestV2 = feature.PublishDigestV2
var publishedRedirectFacts = feature.PublishedRedirectFacts
var classifyPublishedRedirect = feature.ClassifyPublishedRedirect
