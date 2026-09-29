//go:build linux

package privilegedbroker

import (
	"errors"
	"os"
	"syscall"
)

type requestReceiveClass string

const (
	receiveTargetInvalid requestReceiveClass = "receive_target_invalid"
	receiveUnavailable   requestReceiveClass = "receive_unavailable"
	receiveFailed        requestReceiveClass = "receive_failed"
	receiveEmpty         requestReceiveClass = "receive_empty"
	controlTruncated     requestReceiveClass = "control_truncated"
	frameTruncated       requestReceiveClass = "frame_truncated"
	controlMalformed     requestReceiveClass = "control_malformed"
	controlUnsupported   requestReceiveClass = "control_unsupported"
	descriptorForbidden  requestReceiveClass = "descriptor_transfer_forbidden"
	credentialsMissing   requestReceiveClass = "writer_credentials_missing"
	credentialsMalformed requestReceiveClass = "writer_credentials_malformed"
	credentialsAmbiguous requestReceiveClass = "writer_credentials_ambiguous"
	writerChanged        requestReceiveClass = "writer_identity_changed"
	frameLengthInvalid   requestReceiveClass = "frame_length_invalid"
	frameOversized       requestReceiveClass = "frame_oversized"
	frameIncomplete      requestReceiveClass = "frame_incomplete"
	frameMultiple        requestReceiveClass = "frame_multiple"
	frameDecodeInvalid   requestReceiveClass = "frame_decode_invalid"
)

type requestReceiveError struct {
	class requestReceiveClass
	cause error
}

func requestReadFailure(class requestReceiveClass, cause error) error {
	return &requestReceiveError{class: class, cause: cause}
}

func (e *requestReceiveError) Error() string {
	return "broker request receive failed: " + string(e.class)
}
func (e *requestReceiveError) Unwrap() error { return e.cause }

func (e *requestReceiveError) BrokerDiagnostic() (owner, reason, stage, errno, proofMethod string) {
	class := e.class
	switch class {
	case receiveTargetInvalid, receiveUnavailable, receiveFailed, receiveEmpty,
		controlTruncated, frameTruncated, controlMalformed, controlUnsupported, descriptorForbidden,
		credentialsMissing, credentialsMalformed, credentialsAmbiguous, writerChanged,
		frameLengthInvalid, frameOversized, frameIncomplete, frameMultiple, frameDecodeInvalid:
	default:
		class = receiveFailed
	}
	switch {
	case errors.Is(e.cause, syscall.ECONNRESET):
		errno = "ECONNRESET"
	case errors.Is(e.cause, syscall.EBADF):
		errno = "EBADF"
	case errors.Is(e.cause, syscall.EINTR):
		errno = "EINTR"
	case errors.Is(e.cause, os.ErrDeadlineExceeded):
		errno = "DEADLINE"
	case e.cause != nil && (class == receiveFailed || class == receiveUnavailable):
		errno = "OTHER"
	}
	return "broker_transport", string(class), "request_receive", errno, ""
}
