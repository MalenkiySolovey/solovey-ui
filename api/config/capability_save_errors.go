package config

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

	"github.com/gin-gonic/gin"
)

func (a *Handler) handleCapabilitySaveError(c *gin.Context, err error) bool {
	var classified interface{ ReasonCode() string }
	if !errors.As(err, &classified) {
		return false
	}
	var semantic *diagnostics.Rejection
	if errors.As(err, &semantic) {
		a.JSONObj(c, map[string]any{"reason": semantic.ReasonCode(), "findings": []diagnostics.Finding{semantic.Finding}}, err)
		return true
	}
	a.JSONObj(c, map[string]any{"reason": classified.ReasonCode()}, err)
	return true
}
