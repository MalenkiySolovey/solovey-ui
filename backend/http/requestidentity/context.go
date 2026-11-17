// Package requestidentity shares the canonical request-scoped client identity.
package requestidentity

import (
	clientidentity "github.com/MalenkiySolovey/solovey-ui/backend/security/clientidentity"
	"github.com/gin-gonic/gin"
)

const ContextKey = "sui.client_identity.v1"

func FromContext(c *gin.Context) clientidentity.V1 {
	if cached, ok := c.Get(ContextKey); ok {
		if identity, typeOK := cached.(clientidentity.V1); typeOK {
			return identity
		}
	}
	identity := clientidentity.ResolveRequest(c.Request)
	c.Set(ContextKey, identity)
	return identity
}

func IsHTTPS(c *gin.Context) bool { return FromContext(c).DesiredScheme == "https" }
