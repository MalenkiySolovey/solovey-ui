//go:build !minimal

package web

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

var (
	InitTestRouterDB      = initWebRouterTestDB
	SetTestRouterSetting  = setWebRouterSetting
	StopTestRouterWriters = stopWebRouterTestWriters
	LoginTestRouterAtBase = loginRuntimeTransportFixtureAtBase
)

func NewTestRouter(runtime *service.Runtime) (http.Handler, error) {
	panel, err := NewServer(WithRuntime(runtime), WithComponents(productComponentFixture(runtime)))
	if err != nil {
		return nil, err
	}
	return panel.initRouter()
}
