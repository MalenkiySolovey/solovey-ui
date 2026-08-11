//go:build !minimal

package api

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	fallbackcomponent "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"github.com/gin-gonic/gin"
)

const (
	assetUploadReadLimit       = 1024*1024 + 1
	legacySelfStealBodyLimit   = 64 * 1024
	legacySelfStealRetiredCode = "legacy_self_steal_retired"
)

type Deps struct {
	Service        *fallbackcomponent.Service
	ProviderStatus func(context.Context, uint) (ProviderStatusView, error)
	RequireScope   func(*gin.Context, string, ...string) bool
	Actor          func(*gin.Context) string
	JSONObj        func(*gin.Context, interface{}, error)
	JSONMsg        func(*gin.Context, string, error)
}

type Handler struct {
	deps Deps
}

func RegisterRoutes(group *gin.RouterGroup, deps Deps) {
	handler := Handler{deps: deps}
	routes := group.Group("/components/fallback-html")
	routes.GET("/health", handler.health)
	routes.GET("/ports", handler.ports)
	routes.GET("/runtimes", handler.runtimes)
	routes.GET("/templates", handler.templates)
	routes.GET("/template-catalog", handler.remoteTemplateCatalog)
	routes.POST("/template-catalog/:templateId/install", handler.installRemoteTemplate)
	routes.DELETE("/template-catalog/:templateId", handler.deleteRemoteTemplate)
	routes.POST("/templates/:templateId/create-site", handler.createSiteFromTemplate)
	routes.GET("/sites", handler.listSites)
	routes.POST("/sites", handler.saveSite)
	routes.GET("/sites/:id", handler.getSite)
	routes.PUT("/sites/:id", handler.updateSite)
	routes.DELETE("/sites/:id", handler.deleteSite)
	routes.GET("/sites/:id/targets", handler.listTargets)
	routes.GET("/sites/:id/provider-status", handler.providerStatus)
	routes.POST("/sites/:id/targets", handler.saveTarget)
	routes.PUT("/sites/:id/targets/:targetId", handler.updateTarget)
	routes.DELETE("/sites/:id/targets/:targetId", handler.deleteTarget)
	routes.GET("/sites/:id/redirects", handler.listRedirects)
	routes.POST("/sites/:id/redirects", handler.saveRedirect)
	routes.DELETE("/sites/:id/redirects/:redirectId", handler.deleteRedirect)
	routes.GET("/sites/:id/assets", handler.listAssets)
	routes.POST("/sites/:id/assets", handler.uploadAsset)
	routes.DELETE("/sites/:id/assets/:assetId", handler.deleteAsset)
	routes.GET("/sites/:id/external-resources", handler.listExternalResources)
	routes.POST("/sites/:id/external-resources", handler.saveExternalResource)
	routes.DELETE("/sites/:id/external-resources/:resourceId", handler.deleteExternalResource)
	routes.GET("/sites/:id/publishes", handler.listPublishes)
	routes.POST("/sites/:id/publishes/prune", handler.prunePublishes)
	routes.GET("/sites/:id/artifact/:version", handler.downloadArtifact)
	routes.POST("/sites/:id/import", handler.importSite)
	routes.GET("/sites/:id/pages", handler.listPages)
	routes.POST("/sites/:id/pages", handler.savePage)
	routes.PUT("/sites/:id/pages/:pageId", handler.updatePage)
	routes.DELETE("/sites/:id/pages/:pageId", handler.deletePage)
	routes.POST("/sites/:id/path/validate", handler.validatePath)
	routes.POST("/sites/:id/safety", handler.safety)
	routes.POST("/sites/:id/self-steal/draft", handler.createSelfStealDraft)
	routes.POST("/sites/:id/preview", handler.preview)
	routes.POST("/sites/:id/publish", handler.publish)
	routes.POST("/sites/:id/rollback", handler.rollback)
	routes.POST("/sites/:id/unpublish", handler.unpublish)
}

type ProviderReservationStateView struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type ProviderStatusView struct {
	TargetID           string                         `json:"targetId"`
	EndpointMode       string                         `json:"endpointMode"`
	Readiness          string                         `json:"readiness"`
	HealthFreshness    string                         `json:"healthFreshness"`
	HealthObservedAt   int64                          `json:"healthObservedAt"`
	HealthExpiresAt    int64                          `json:"healthExpiresAt"`
	CapacityState      string                         `json:"capacityState"`
	CapacitySlotsUsed  uint32                         `json:"capacitySlotsUsed"`
	CapacitySlotsTotal uint32                         `json:"capacitySlotsTotal"`
	InUse              bool                           `json:"inUse"`
	ReconcileRequired  bool                           `json:"reconcileRequired"`
	Reservations       []ProviderReservationStateView `json:"reservations"`
	ReasonCodes        []string                       `json:"reasonCodes"`
}

func (h Handler) health(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	h.deps.JSONObj(c, h.deps.Service.RuntimeHealth(), nil)
}

func (h Handler) ports(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	result, err := h.deps.Service.PortCandidates()
	h.deps.JSONObj(c, result, err)
}

func (h Handler) runtimes(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	h.deps.JSONObj(c, h.deps.Service.RuntimeOptions(), nil)
}

func (h Handler) templates(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	h.deps.JSONObj(c, h.deps.Service.ListTemplates(), nil)
}

func (h Handler) remoteTemplateCatalog(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	result, err := h.deps.Service.ListRemoteTemplateCatalog(c.Request.Context())
	if errors.Is(err, context.Canceled) {
		c.Status(499)
		return
	}
	h.deps.JSONObj(c, result, err)
}

func (h Handler) installRemoteTemplate(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	result, err := h.deps.Service.InstallRemoteTemplate(c.Request.Context(), c.Param("templateId"), h.deps.Actor(c))
	if errors.Is(err, context.Canceled) {
		c.Status(499)
		return
	}
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deleteRemoteTemplate(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteRemoteTemplate(c.Param("templateId"), h.deps.Actor(c)))
}

func (h Handler) createSiteFromTemplate(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	result, err := h.deps.Service.CreateSiteFromTemplate(c.Param("templateId"), h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) listSites(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	result, err := h.deps.Service.ListSites()
	h.deps.JSONObj(c, result, err)
}

func (h Handler) saveSite(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	var input fallbackcomponent.SiteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SaveSite(input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) updateSite(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.SiteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	input.ID = id
	result, err := h.deps.Service.SaveSite(input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) getSite(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.GetSite(id)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deleteSite(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteSite(id, h.deps.Actor(c)))
}

func (h Handler) listTargets(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListTargets(id)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) providerStatus(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	if h.deps.ProviderStatus == nil {
		h.deps.JSONObj(c, ProviderStatusView{}, errors.New("provider status is unavailable"))
		return
	}
	result, err := h.deps.ProviderStatus(c.Request.Context(), id)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) saveTarget(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.TargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SaveTarget(id, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) updateTarget(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	targetID, ok := pathUint(c, "targetId")
	if !ok {
		return
	}
	var input fallbackcomponent.TargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	input.ID = targetID
	result, err := h.deps.Service.SaveTarget(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deleteTarget(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	targetID, ok := pathUint(c, "targetId")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteTarget(siteID, targetID, h.deps.Actor(c)))
}

func (h Handler) listRedirects(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListRedirects(id)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) saveRedirect(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.RedirectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SaveRedirect(id, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deleteRedirect(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	redirectID, ok := pathUint(c, "redirectId")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteRedirect(siteID, redirectID, h.deps.Actor(c)))
}

func (h Handler) listAssets(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListAssets(siteID)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) uploadAsset(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if err := validateAssetUploadMultipart(c.Request.MultipartForm); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	header, err := c.FormFile("file")
	if err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	file, err := header.Open()
	if err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, assetUploadReadLimit))
	if err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SaveAsset(siteID, header.Filename, data, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func validateAssetUploadMultipart(form *multipart.Form) error {
	if form == nil || len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return errors.New("asset upload requires exactly one file")
	}
	return nil
}

func (h Handler) deleteAsset(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	assetID, ok := pathUint(c, "assetId")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteAsset(siteID, assetID, h.deps.Actor(c)))
}

func (h Handler) listExternalResources(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListExternalResources(siteID)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) saveExternalResource(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.ExternalResourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SaveExternalResource(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deleteExternalResource(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	resourceID, ok := pathUint(c, "resourceId")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeleteExternalResource(siteID, resourceID, h.deps.Actor(c)))
}

func (h Handler) listPublishes(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListPublishes(siteID)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) prunePublishes(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.PrunePublishesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.PrunePublishes(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) downloadArtifact(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	artifact, err := h.deps.Service.GetPublishArtifact(siteID, c.Param("version"))
	if err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+artifact.Filename+`"`)
	c.Data(http.StatusOK, artifact.ContentType, artifact.Data)
}

func (h Handler) importSite(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.SiteImportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.ImportSite(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) listPages(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.ListPages(siteID)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) savePage(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.PageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.SavePage(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) updatePage(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	pageID, ok := pathUint(c, "pageId")
	if !ok {
		return
	}
	var input fallbackcomponent.PageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	input.ID = pageID
	result, err := h.deps.Service.SavePage(siteID, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) deletePage(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	pageID, ok := pathUint(c, "pageId")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "del", h.deps.Service.DeletePage(siteID, pageID, h.deps.Actor(c)))
}

func (h Handler) validatePath(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	siteID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.PathValidationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.ValidatePath(siteID, input)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) safety(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.Safety(id)
	h.deps.JSONObj(c, result, err)
}

func (h Handler) createSelfStealDraft(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, legacySelfStealBodyLimit)
	if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"success": false,
			"msg":     legacySelfStealRetiredCode,
			"obj": gin.H{
				"code":    legacySelfStealRetiredCode,
				"message": "Native fallback is managed through the protection workflow.",
			},
		})
		return
	}
	c.JSON(http.StatusGone, gin.H{
		"success": false,
		"msg":     legacySelfStealRetiredCode,
		"obj": gin.H{
			"code":    legacySelfStealRetiredCode,
			"message": "Native fallback is managed through the protection workflow.",
		},
	})
}

func (h Handler) preview(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.PreviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.PreviewSite(id, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) publish(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	result, err := h.deps.Service.PublishSite(id, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) rollback(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var input fallbackcomponent.RollbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.deps.JSONMsg(c, "fallbackHtml", err)
		return
	}
	result, err := h.deps.Service.RollbackSite(id, input, h.deps.Actor(c))
	h.deps.JSONObj(c, result, err)
}

func (h Handler) unpublish(c *gin.Context) {
	if !h.deps.RequireScope(c, "publicSite", "admin", "write", "public-site") {
		return
	}
	id, ok := pathUint(c, "id")
	if !ok {
		return
	}
	h.deps.JSONMsg(c, "update", h.deps.Service.UnpublishSite(id, h.deps.Actor(c)))
}

func pathUint(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid " + name})
		return 0, false
	}
	return uint(value), true
}
