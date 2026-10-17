package api

import (
	"context"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	identity "github.com/MalenkiySolovey/solovey-ui/internal/networkidentity"
	"github.com/MalenkiySolovey/solovey-ui/service/resourceinventory"
	"github.com/gin-gonic/gin"
)

type networkIdentityReader interface {
	LocalHostNetworkIdentity(context.Context, hostresources.ResourceSnapshot) identity.Snapshot
}

type networkIdentityHTTP struct {
	reader    networkIdentityReader
	resources func(context.Context) hostresources.ResourceSnapshot
}

func (a *APIHandler) registerNetworkIdentityRoutes(g *gin.RouterGroup) {
	h := &networkIdentityHTTP{reader: resourceinventory.NewHostIngressScopeProviderV1(), resources: hostresources.Snapshot}
	g.GET("/v1/network-identity", h.snapshot)
}

func (h *networkIdentityHTTP) snapshot(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, h.reader.LocalHostNetworkIdentity(c.Request.Context(), h.resources(c.Request.Context())), nil)
}
