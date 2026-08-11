package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// ReleaseTrustRootsJSON is injected by a production release build after
// public-root custody is established. An empty value is intentionally
// fail-closed and is surfaced as SIGNING_UNAVAILABLE. Private keys are never
// accepted by panel configuration or embedded in the binary.
var ReleaseTrustRootsJSON string
var ReleaseTrustRootsBase64 string

type encodedRoot struct {
	KeyID       string            `json:"keyId"`
	PublicKey   string            `json:"publicKey"`
	State       release.RootState `json:"state"`
	NotBefore   int64             `json:"notBefore"`
	NotAfter    int64             `json:"notAfter"`
	MinSequence uint64            `json:"minSequence"`
	MaxSequence uint64            `json:"maxSequence,omitempty"`
}

func ReleaseTrustStore() (release.TrustStore, error) {
	payload := []byte(ReleaseTrustRootsJSON)
	if ReleaseTrustRootsBase64 != "" {
		decoded, err := base64.StdEncoding.Strict().DecodeString(ReleaseTrustRootsBase64)
		if err != nil || len(decoded) == 0 || len(decoded) > 64<<10 {
			return release.TrustStore{}, errors.New("production release trust roots encoding is invalid")
		}
		payload = decoded
	}
	if len(payload) == 0 {
		return release.TrustStore{}, errors.New("production release trust roots are not configured")
	}
	var encoded []encodedRoot
	if err := json.Unmarshal(payload, &encoded); err != nil || len(encoded) == 0 || len(encoded) > 8 {
		return release.TrustStore{}, errors.New("production release trust roots are invalid")
	}
	roots := make([]release.TrustRoot, 0, len(encoded))
	for _, value := range encoded {
		publicKey, err := base64.StdEncoding.Strict().DecodeString(value.PublicKey)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return release.TrustStore{}, errors.New("production release public key is invalid")
		}
		roots = append(roots, release.TrustRoot{KeyID: value.KeyID, PublicKey: ed25519.PublicKey(publicKey), State: value.State,
			NotBefore: time.Unix(value.NotBefore, 0).UTC(), NotAfter: time.Unix(value.NotAfter, 0).UTC(),
			MinSequence: value.MinSequence, MaxSequence: value.MaxSequence})
	}
	return release.NewTrustStore(roots)
}
