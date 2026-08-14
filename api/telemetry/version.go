package telemetry

import (
	"github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
)

// versionInfo preserves the historical read-only response shape while using
// the signed update lifecycle as its only release authority.
type versionInfo struct {
	Current         string `json:"current"`
	Version         string `json:"version"`
	Channel         string `json:"channel"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	AssetAvailable  bool   `json:"assetAvailable,omitempty"`
	CheckedAt       int64  `json:"checkedAt,omitempty"`
	CheckError      string `json:"checkError,omitempty"`
}

func (a *Handler) GetVersionInfo(c *gin.Context) {
	manager := a.Update
	if manager == nil {
		manager = updateservice.SharedLifecycle()
	}
	posture := manager.Status(c.Request.Context(), release.ChannelMain)
	current := posture.Actual.Version
	if current == "" {
		current = identity.GetVersion()
	}
	result := versionInfo{Current: current, Version: current, Channel: string(posture.Desired.Channel), CheckedAt: posture.ObservedAt}
	if posture.Selected != nil {
		result.Latest = posture.Selected.Version
	}
	result.UpdateAvailable = posture.State == updateservice.StateUpdateAvailable
	result.AssetAvailable = result.UpdateAvailable && posture.SigningStatus == "VERIFIED" && posture.Capabilities.Download == "AVAILABLE"
	if posture.SigningStatus != "VERIFIED" && len(posture.ReasonCodes) > 0 {
		result.CheckError = posture.ReasonCodes[0]
	}
	a.JSONObj(c, result, nil)
}
