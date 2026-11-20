package api

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/composition/componenthttp"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

// These fixtures use Product's real selected-Component adapter. Production
// constructors deliberately require explicit composition from their caller.
func composedOptions(options []Option) []Option {
	// Resolve the selected Runtime before creating the Product-side adapter.
	selected := ApiService{Runtime: service.DefaultRuntime()}
	for _, option := range options {
		if option != nil {
			option(&selected)
		}
	}
	runtime := selected.Runtime
	owners := componenthost.APIDeps{Runtime: runtime, OutboundHTTP: componenthost.OutboundHTTPClientFunc(func(tag string, timeout time.Duration) (*http.Client, error) {
		return service.NewOutboundHTTPClientForRuntime(runtime, tag, timeout)
	})}
	return append(options, WithComponents(componenthttp.New(owners)))
}

func newComposedApiService(options ...Option) ApiService {
	return NewApiService(composedOptions(options)...)
}
func newComposedAPIv2Handler(g *gin.RouterGroup, options ...Option) *APIv2Handler {
	return NewAPIv2Handler(g, composedOptions(options)...)
}
func newComposedAPIHandler(g *gin.RouterGroup, v2 *APIv2Handler, options ...Option) {
	NewAPIHandler(g, v2, composedOptions(options)...)
}
