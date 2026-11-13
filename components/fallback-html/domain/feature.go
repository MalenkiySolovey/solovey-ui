//go:build !minimal

package domain

import feature "github.com/MalenkiySolovey/solovey-component-fallback-html/domain"

// Compatibility names delegate to the immutable component source.
type Site = feature.Site
type Page = feature.Page
type Redirect = feature.Redirect
type Asset = feature.Asset
type Publish = feature.Publish
type PublishFile = feature.PublishFile
type PublishRedirect = feature.PublishRedirect
type SafetyReport = feature.SafetyReport
type TemplateSource = feature.TemplateSource
type SelfStealDraft = feature.SelfStealDraft
type RuntimeTarget = feature.RuntimeTarget
type ExternalResource = feature.ExternalResource
type Event = feature.Event

var NormalizePublicPath = feature.NormalizePublicPath
var ValidatePagePath = feature.ValidatePagePath
var NormalizeRedirectTarget = feature.NormalizeRedirectTarget
var ValidateRedirectStatus = feature.ValidateRedirectStatus
var IsReservedPublicPath = feature.IsReservedPublicPath
var PublicPathToFilePath = feature.PublicPathToFilePath

const DefaultTemplateID = feature.DefaultTemplateID
const ContentModeText = feature.ContentModeText
const ContentModeHTML = feature.ContentModeHTML
const ContentModeStaticHTML = feature.ContentModeStaticHTML

type TemplateData = feature.TemplateData
type TemplateDefinition = feature.TemplateDefinition

var BuiltInTemplates = feature.BuiltInTemplates
var RenderGeneratedPage = feature.RenderGeneratedPage
var NormalizeContentMode = feature.NormalizeContentMode
var ErrUnsupportedContentMode = feature.ErrUnsupportedContentMode
var SanitizeBodyHTML = feature.SanitizeBodyHTML
var ValidateStaticHTML = feature.ValidateStaticHTML
var ValidateDecoyTemplateHTML = feature.ValidateDecoyTemplateHTML
var ValidateStaticCSS = feature.ValidateStaticCSS
