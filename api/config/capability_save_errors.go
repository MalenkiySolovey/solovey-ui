package config

import (
	"errors"

	"github.com/gin-gonic/gin"
)

func (a *Handler) handleCapabilitySaveError(c *gin.Context, err error) bool {
	var classified interface{ ReasonCode() string }
	if !errors.As(err, &classified) {
		return false
	}
	a.JSONObj(c, map[string]any{"reason": classified.ReasonCode()}, err)
	return true
}
