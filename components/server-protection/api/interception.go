//go:build !minimal

package api

import (
	"net/http"
	"strings"

	protectioninterception "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/interception"
	"github.com/gin-gonic/gin"
)

func (h Handler) interceptionStatus(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, err := h.deps.Interception.Status(c.Request.Context())
	if err != nil {
		writeInterceptionError(c, err)
		return
	}
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) interceptionPreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input protectioninterception.PreviewRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	value, err := h.deps.Interception.Preview(c.Request.Context(), input)
	if err != nil {
		writeInterceptionError(c, err)
		return
	}
	h.audit(c, "server_protection_interception_preview", map[string]any{
		"planId": value.PlanID, "resourceId": value.Fact.ResourceID, "endpointId": value.Fact.EndpointID,
		"kind": value.Fact.Kind, "network": value.Fact.Network, "family": value.Fact.AddressFamily,
		"disposition": value.Disposition, "actualState": value.ActualState,
	})
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) interceptionBlockedMutation(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectioninterception.BlockedMutationRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	err := h.deps.Interception.BlockedMutation(input)
	h.audit(c, "server_protection_interception_mutation_rejected", map[string]any{
		"route": c.FullPath(), "code": protectioninterception.ErrorCode(err),
	})
	writeInterceptionError(c, err)
}

func (h Handler) interceptionOperation(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, err := h.deps.Interception.Operation(strings.TrimSpace(c.Param("operationId")))
	if err != nil {
		writeInterceptionError(c, err)
		return
	}
	h.deps.JSONObj(c, value, nil)
}

func writeInterceptionError(c *gin.Context, err error) {
	code := protectioninterception.ErrorCode(err)
	status := http.StatusConflict
	switch code {
	case protectioninterception.CodeMalformedInput:
		status = http.StatusBadRequest
	case protectioninterception.CodeFactMissing, protectioninterception.CodeOperationUnavailable:
		status = http.StatusNotFound
	case protectioninterception.CodeInternalFailure:
		status = http.StatusInternalServerError
	}
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": code}})
}
