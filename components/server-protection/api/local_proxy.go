//go:build !minimal

package api

import (
	"net/http"
	"strings"

	protectionlocalproxy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/localproxy"
	"github.com/gin-gonic/gin"
)

func (h Handler) localProxyStatus(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, err := h.deps.LocalProxy.Status(c.Request.Context(), queryBool(c, "refresh"))
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) localProxyPreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input protectionlocalproxy.PlanReferenceV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	value, err := h.deps.LocalProxy.Preview(c.Request.Context(), input)
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	h.audit(c, "server_protection_local_proxy_preview", map[string]any{
		"planId": value.PlanID, "resourceId": value.ResourceID, "endpointId": value.EndpointID,
		"applyGate": value.ApplyGate, "actualState": value.ActualState,
	})
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) localProxyPrepare(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionlocalproxy.PrepareRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	value, err := h.deps.LocalProxy.Prepare(c.Request.Context(), h.actor(c), input)
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	if !value.Replayed {
		h.audit(c, "server_protection_local_proxy_prepare", map[string]any{
			"operationId": value.OperationID, "planId": value.PlanID, "actualState": value.ActualState,
		})
	}
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) localProxyApply(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionlocalproxy.ApplyRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	value, err := h.deps.LocalProxy.Apply(c.Request.Context(), input)
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	if !value.Replayed {
		h.audit(c, "server_protection_local_proxy_apply", map[string]any{
			"operationId": value.OperationID, "planId": value.PlanID, "actualState": value.ActualState,
			"recoveryRequired": value.RecoveryRequired,
		})
	}
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) localProxyDisable(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionlocalproxy.DisableRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	value, err := h.deps.LocalProxy.Disable(c.Request.Context(), input)
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	if !value.Replayed {
		h.audit(c, "server_protection_local_proxy_disable", map[string]any{
			"operationId": value.OperationID, "actualState": value.ActualState,
		})
	}
	h.deps.JSONObj(c, value, nil)
}

func (h Handler) localProxyOperation(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, err := h.deps.LocalProxy.Operation(c.Request.Context(), strings.TrimSpace(c.Param("operationId")))
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	h.deps.JSONObj(c, makeOperationView(value), nil)
}

func (h Handler) localProxyRecovery(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, err := h.deps.LocalProxy.Recovery(c.Request.Context(), strings.TrimSpace(c.Param("operationId")))
	if err != nil {
		writeLocalProxyError(c, err)
		return
	}
	h.deps.JSONObj(c, value, nil)
}

func writeLocalProxyError(c *gin.Context, err error) {
	code := protectionlocalproxy.ErrorCode(err)
	if code == "" {
		code = protectionlocalproxy.CodeInternalFailure
	}
	status := http.StatusConflict
	switch code {
	case protectionlocalproxy.CodeMalformedInput, protectionlocalproxy.CodeConfirmationRequired,
		protectionlocalproxy.CodeAcknowledgementRequired:
		status = http.StatusBadRequest
	case protectionlocalproxy.CodeOperationNotFound, protectionlocalproxy.CodeFactMissing:
		status = http.StatusNotFound
	case protectionlocalproxy.CodeInternalFailure:
		status = http.StatusInternalServerError
	}
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": code}})
}
