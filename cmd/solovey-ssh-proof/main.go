//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "SSH reconnect proof failed:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 {
		return errors.New("arguments are not supported")
	}
	if os.Getuid() <= 0 {
		return errors.New("run this fixed proof command from the fresh non-root SSH session")
	}
	// The installed binary is setgid only to the dedicated broker-client
	// group. Normalize all GID identities before connecting so SO_PEERCRED and
	// /proc identity agree; no UID or root authority is acquired.
	proofGID := os.Getegid()
	if proofGID <= 0 || unix.Setresgid(proofGID, proofGID, proofGID) != nil {
		return errors.New("dedicated SSH proof group is unavailable")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	randomDigest := broker.Digest(random)
	now := time.Now().UTC()
	client := broker.NewClient(broker.RoleSSHProof)
	var result sshbroker.ProofResultV1
	_, err := client.Invoke(context.Background(), broker.Call{Verb: broker.VerbSSHProof, OperationID: "ssh-proof-session",
		IdempotencyKey: "proof-" + hex.EncodeToString(random[:24]), Fence: broker.Fence{
			Resource: fmt.Sprintf("ssh-proof-uid-%d", os.Getuid()), Sequence: uint64(now.UnixNano()), Token: randomDigest},
		Timeout: 15 * time.Second, Payload: sshbroker.ProofRequestV1{}}, &result)
	if err != nil {
		return err
	}
	// This one-time reference is intentionally emitted only to the authenticated
	// SSH terminal. It never crosses the panel HTTP API or its audit log.
	return json.NewEncoder(os.Stdout).Encode(struct {
		OperationID         string `json:"operationId"`
		ProviderEvidenceRef string `json:"providerEvidenceRef"`
		ExpiresAt           int64  `json:"expiresAt"`
	}{result.OperationID, result.Verifier, result.ExpiresAt})
}
