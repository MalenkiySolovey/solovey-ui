package web

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-backend/componentroutes"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/composition/componenthttp"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"net/http"
	"testing"
	"time"
)

func TestPanelRouterRequiresExplicitProductComponentComposition(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	router, err := server.initRouter()
	if router != nil || !errors.Is(err, componentroutes.ErrUnavailable) {
		t.Fatalf("missing Product input started Panel HTTP: %v %v", router, err)
	}
}

func productComponentFixture(runtime *service.Runtime) *componenthttp.Adapter {
	return componenthttp.New(componenthost.APIDeps{Runtime: runtime, OutboundHTTP: componenthost.OutboundHTTPClientFunc(func(tag string, timeout time.Duration) (*http.Client, error) {
		return service.NewOutboundHTTPClientForRuntime(runtime, tag, timeout)
	})}.WithObservationOwners())
}
