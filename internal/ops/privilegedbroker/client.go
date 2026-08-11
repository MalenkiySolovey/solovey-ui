package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

type DialFunc func(context.Context, string) (net.Conn, error)

type Client struct {
	SocketPath string
	Role       Role
	Dial       DialFunc
	Verify     func(net.Conn, string) error
	Now        func() time.Time
	BootID     string
	sequence   atomic.Uint64
}

type Call struct {
	Verb           Verb
	OperationID    string
	IdempotencyKey string
	Fence          Fence
	Expected       Revisions
	Purpose        string
	RecoveryRef    string
	Timeout        time.Duration
	Payload        any
}

func NewClient(role Role) *Client {
	path := DefaultSocketPath
	if role == RoleSSHProof {
		path = ProofSocketPath
	}
	return &Client{SocketPath: path, Role: role, Now: time.Now}
}

func (c *Client) Invoke(ctx context.Context, call Call, target any) (*Receipt, error) {
	if c == nil || c.Role != RolePanel && c.Role != RoleSSHProof {
		return nil, errors.New("broker client is not configured")
	}
	payload, payloadDigest, err := MarshalPayload(call.Payload)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	bootID := c.BootID
	if bootID == "" {
		bootID, err = currentBootID()
		if err != nil {
			return nil, errors.New("broker client boot identity is unavailable")
		}
	}
	timeout := call.Timeout
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 15 * time.Second
	}
	requestID := fmt.Sprintf("req-%d-%d", now.UnixMilli(), c.sequence.Add(1))
	request := Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		BootID: bootID, Role: c.Role, Verb: call.Verb, RequestID: requestID, OperationID: call.OperationID,
		IdempotencyKey: call.IdempotencyKey, Fence: call.Fence, Expected: call.Expected,
		DeadlineAt: now.Add(timeout).UnixMilli(), Purpose: call.Purpose, RecoveryRef: call.RecoveryRef,
		PayloadDigest: payloadDigest, Payload: payload}
	dial := c.Dial
	if dial == nil {
		dialer := &net.Dialer{Timeout: minDuration(timeout, 5*time.Second)}
		dial = func(ctx context.Context, path string) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }
	}
	connection, err := dial(ctx, c.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("connect privileged broker: %w", err)
	}
	defer connection.Close()
	verify := c.Verify
	if verify == nil && c.Dial == nil {
		verify = verifyServerConnection
	}
	if verify != nil {
		if err := verify(connection, c.SocketPath); err != nil {
			return nil, fmt.Errorf("attest privileged broker: %w", err)
		}
	}
	deadline := now.Add(timeout)
	_ = connection.SetDeadline(deadline)
	if err := WriteFrame(connection, request, MaxRequestBytes); err != nil {
		return nil, fmt.Errorf("write privileged broker request: %w", err)
	}
	var response Response
	if err := ReadFrame(connection, &response, MaxResponseBytes); err != nil {
		return nil, fmt.Errorf("read privileged broker response: %w", err)
	}
	if response.ProtocolVersion != ProtocolVersion || response.CapabilityRevision != CapabilityRevision ||
		response.RequestID != request.RequestID || response.OperationID != request.OperationID || response.Verb != request.Verb {
		return nil, errors.New("privileged broker response identity mismatch")
	}
	if !response.OK {
		return response.Receipt, &PublicError{Code: response.Code, Safe: response.Message}
	}
	if len(response.Payload) > 0 {
		if response.PayloadDigest != Digest(response.Payload) {
			return response.Receipt, errors.New("privileged broker response digest mismatch")
		}
		if target != nil {
			if err := DecodePayload(response.Payload, target); err != nil {
				return response.Receipt, fmt.Errorf("decode privileged broker payload: %w", err)
			}
		}
	} else if target != nil {
		return response.Receipt, errors.New("privileged broker response payload is absent")
	}
	return response.Receipt, nil
}

func DecodeRawPayload(raw json.RawMessage, target any) error { return DecodePayload(raw, target) }

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
