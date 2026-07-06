//go:build !minimal

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"

	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/web/publicsurface"
	"github.com/gin-gonic/gin"
)

type RuntimeHealth struct {
	RuntimeStatus
	OK             bool     `json:"ok"`
	Issues         []string `json:"issues,omitempty"`
	HomeStatus     int      `json:"homeStatus,omitempty"`
	NotFoundStatus int      `json:"notFoundStatus,omitempty"`
	AdminReserved  bool     `json:"adminReserved"`
	ACMEReserved   bool     `json:"acmeReserved"`
}

type runtimeProbe struct {
	Handled bool
	Status  int
}

func (s *Service) RuntimeHealth() RuntimeHealth {
	settings := coreservice.SettingService{}
	adminBasePath, err := settings.GetWebPath()
	if strings.TrimSpace(adminBasePath) == "" {
		adminBasePath = "/app/"
	}
	report := s.runtime.Health(publicsurface.Context{AdminBasePath: adminBasePath})
	if err != nil {
		report.addIssue("panel web path is unavailable: " + err.Error())
		report.OK = false
	}
	return report
}

func (r *Runtime) Health(ctx publicsurface.Context) RuntimeHealth {
	status := r.Status()
	report := RuntimeHealth{RuntimeStatus: status}
	if !status.Active {
		report.addIssue("no active published site")
		return report
	}

	home := r.probe(ctx, http.MethodGet, "/")
	report.HomeStatus = home.Status
	if !home.Handled {
		report.addIssue("home path was not handled")
	} else if home.Status != http.StatusOK {
		report.addIssue("home path returned " + http.StatusText(home.Status))
	}

	notFound := r.probe(ctx, http.MethodGet, "/__solovey_fallback_health_missing__/")
	report.NotFoundStatus = notFound.Status
	if !notFound.Handled {
		report.addIssue("missing page was not handled by the public 404 page")
	} else if notFound.Status != http.StatusNotFound {
		report.addIssue("missing page returned " + http.StatusText(notFound.Status))
	}

	admin := r.probe(ctx, http.MethodGet, ctx.AdminBasePath)
	report.AdminReserved = !admin.Handled
	if admin.Handled {
		report.addIssue("admin base path was handled by public site")
	}

	acme := r.probe(ctx, http.MethodGet, "/.well-known/acme-challenge/health")
	report.ACMEReserved = !acme.Handled
	if acme.Handled {
		report.addIssue("ACME challenge path was handled by public site")
	}

	report.OK = len(report.Issues) == 0
	return report
}

func (r *Runtime) probe(ctx publicsurface.Context, method string, path string) runtimeProbe {
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, path, nil)
	handled := r.servePublic(ginCtx, ctx, false)
	return runtimeProbe{Handled: handled, Status: recorder.Code}
}

func (r *RuntimeHealth) addIssue(issue string) {
	if strings.TrimSpace(issue) == "" {
		return
	}
	r.Issues = append(r.Issues, issue)
	r.OK = false
}
