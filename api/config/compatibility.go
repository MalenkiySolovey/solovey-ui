package config

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
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
		Config json.RawMessage `json:"config"`
	}
	if c.ShouldBindJSON(&request) != nil || len(request.Config) == 0 {
		a.JSONMsg(c, "config must be a JSON object", diagnostics.FirstError([]diagnostics.Finding{{Path: "config", Code: "invalid_json", Severity: diagnostics.Error, Message: "Configuration must be a JSON object."}}))
		return
	}
	projection, err := service.NewSingBoxConfigBuilder(a.Runtime).BuildCandidateProjectionFromDB(dbsqlite.DB(), string(request.Config), true)
	findings := append(projection.RuleCompatibility, projection.DNSCompatibility...)
	if err != nil && diagnostics.FirstError(findings) == nil {
		findings = append(findings, diagnostics.Finding{Kind: "config", Path: "config", Code: "candidate_preparation_failed", Severity: diagnostics.Error, Message: "The complete candidate could not be prepared. Check database and entity references in Doctor before retrying.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
	}
	outcome := diagnostics.LosslessAutomatic
	if len(findings) > 0 {
		outcome = diagnostics.AutomaticDiagnostic
	}
	if err != nil {
		outcome = diagnostics.ManualRequired
	}
	for _, f := range findings {
		if f.Severity == diagnostics.Error && f.MigrationOutcome == diagnostics.UnsupportedLegacy {
			outcome = diagnostics.UnsupportedLegacy
		}
	}
	// Return only the DNS section the operator is actively editing. Never return
	// assembled entity credentials, certificate providers, or full DB state.
	var root map[string]json.RawMessage
	if err == nil {
		_ = json.Unmarshal(projection.Config, &root)
	}
	a.JSONObj(c, struct {
		Outcome  string                `json:"outcome"`
		Findings []diagnostics.Finding `json:"findings"`
		DNS      json.RawMessage       `json:"dns,omitempty"`
		Blocked  bool                  `json:"blocked"`
	}{outcome, findings, root["dns"], err != nil}, nil)
}
