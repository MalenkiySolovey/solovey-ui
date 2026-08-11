package localproxy

import (
	"errors"
	"strings"
)

const (
	CodeMalformedInput                = "MALFORMED_INPUT"
	CodeConfirmationRequired          = "CONFIRMATION_REQUIRED"
	CodeAcknowledgementRequired       = "EXPERIMENTAL_ACK_REQUIRED"
	CodeFactMissing                   = "FACT_MISSING"
	CodeFactNotActionable             = "FACT_NOT_ACTIONABLE"
	CodeRevisionDrift                 = "REVISION_DRIFT"
	CodeRuntimeDrift                  = "RUNTIME_DRIFT"
	CodeLeaseDrift                    = "LEASE_DRIFT"
	CodeProviderUnavailable           = "PROVIDER_UNAVAILABLE"
	CodeMissingHealth                 = "BLOCKED_MISSING_HEALTH"
	CodeNotShipped                    = "BLOCKED_NOT_SHIPPED"
	CodeExternalManaged               = "EXTERNAL_MANAGED"
	CodeOwnerUnproven                 = "OWNER_UNPROVEN"
	CodeListenerUnproven              = "LISTENER_UNPROVEN"
	CodeCapacityUnavailable           = "CAPACITY_UNAVAILABLE"
	CodeManagementCollision           = "MANAGEMENT_LISTENER_COLLISION"
	CodeRecoveryCollision             = "RECOVERY_PATH_COLLISION"
	CodeAuthenticationUnknown         = "AUTHENTICATION_UNKNOWN"
	CodePrivateAuthenticationRequired = "PRIVATE_AUTHENTICATION_REQUIRED"
	CodeRuntimeShapeUnknown           = "RUNTIME_SHAPE_UNKNOWN"
	CodeStateInvalid                  = "STATE_INVALID"
	CodeOperationNotFound             = "OPERATION_NOT_FOUND"
	CodeOperationConflict             = "OPERATION_CONFLICT"
	CodeHealthFailed                  = "HEALTH_FAILED"
	CodeRecoveryRequired              = "RECOVERY_REQUIRED"
	CodeInternalFailure               = "INTERNAL_FAILURE"
)

type codedError struct {
	code string
}

func (e codedError) Error() string { return e.code }

func serviceError(code string) error { return codedError{code: code} }

func ErrorCode(err error) string {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return ""
}

func validateMutationToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= limit && !strings.ContainsAny(value, "\x00\r\n\t")
}
