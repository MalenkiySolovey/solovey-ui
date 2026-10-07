package config

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func (a *Handler) GetEditorContract(c *gin.Context) {
	if a.RequireScope != nil && !a.RequireScope(c, "config", "admin", "read", "write") {
		return
	}
	contract := singboxconfig.EditorContract()
	contract.TUNDNSUnavailableModes = entityinbounds.TUNDNSUnavailableModes()
	a.JSONObj(c, contract, nil)
}

func (a *Handler) PreviewCompatibility(c *gin.Context) {
	if a.RequireScope != nil && !a.RequireScope(c, "config", "admin", "read", "write") {
		return
	}
	var request struct {
		Config               json.RawMessage `json:"config"`
		IncludeHTTP          bool            `json:"includeHttp"`
		SubscriptionTemplate json.RawMessage `json:"subscriptionTemplate"`
	}
	if c.ShouldBindJSON(&request) != nil || (len(request.Config) == 0 && len(request.SubscriptionTemplate) == 0) || (len(request.Config) > 0 && len(request.SubscriptionTemplate) > 0) {
		a.JSONMsg(c, "config must be a JSON object", diagnostics.FirstError([]diagnostics.Finding{{Path: "config", Code: "invalid_json", Severity: diagnostics.Error, Message: "Configuration must be a JSON object."}}))
		return
	}
	if len(request.SubscriptionTemplate) > 0 {
		prepared, err := subformats.PrepareJSONExtensionUpgrade(request.SubscriptionTemplate)
		var candidate json.RawMessage
		if err == nil {
			candidate = prepared.Candidate
		}
		a.JSONObj(c, struct {
			Outcome  string                `json:"outcome"`
			Findings []diagnostics.Finding `json:"findings"`
			Blocked  bool                  `json:"blocked"`
			Template json.RawMessage       `json:"subscriptionTemplate,omitempty"`
		}{prepared.Outcome, prepared.Findings, err != nil, candidate}, nil)
		return
	}
	projection, err := service.NewSingBoxConfigBuilder(a.Runtime).BuildCandidateProjectionFromDB(dbsqlite.DB(), string(request.Config), true)
	findings := append(projection.RuleCompatibility, projection.DNSCompatibility...)
	findings = append(findings, projection.HTTPCompatibility...)
	if err != nil && diagnostics.FirstError(findings) == nil {
		findings = append(findings, diagnostics.Finding{Kind: "config", Path: "config", Code: "candidate_preparation_failed", Severity: diagnostics.Error, Message: "The complete candidate could not be prepared. Check database and entity references in Doctor before retrying.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
	}
	outcome := diagnostics.Outcome(findings)
	// Return only the requested editable domains. Never return assembled entity
	// credentials, certificate providers, or full database state.
	var root map[string]json.RawMessage
	if err == nil {
		_ = json.Unmarshal(projection.Config, &root)
	}
	var httpClients, route json.RawMessage
	if request.IncludeHTTP {
		httpClients, route = root["http_clients"], root["route"]
	}
	a.JSONObj(c, struct {
		Outcome     string                `json:"outcome"`
		Findings    []diagnostics.Finding `json:"findings"`
		DNS         json.RawMessage       `json:"dns,omitempty"`
		HTTPClients json.RawMessage       `json:"http_clients,omitempty"`
		Route       json.RawMessage       `json:"route,omitempty"`
		Blocked     bool                  `json:"blocked"`
	}{outcome, findings, root["dns"], httpClients, route, err != nil}, nil)
}
