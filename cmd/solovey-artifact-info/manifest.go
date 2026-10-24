package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// These packaging-only modes reuse the runtime release owner. Canonicalizing
// input or observing a valid signature grants no installation authority.
func canonicalManifest(input io.Reader, output io.Writer) error {
	var manifest release.Manifest
	if err := decodePackagingInput(input, release.MaxManifestBytes, &manifest); err != nil {
		return err
	}
	canonical, err := release.Canonical(manifest)
	if err != nil {
		return err
	}
	_, err = output.Write(canonical)
	return err
}

type packagingTrustRoot struct {
	KeyID       string            `json:"keyId"`
	PublicKey   string            `json:"publicKey"`
	State       release.RootState `json:"state"`
	NotBefore   int64             `json:"notBefore"`
	NotAfter    int64             `json:"notAfter"`
	MinSequence uint64            `json:"minSequence"`
	MaxSequence uint64            `json:"maxSequence,omitempty"`
}

func verifyRelease(candidate string, input io.Reader, output io.Writer) error {
	var request struct {
		TrustRoots []packagingTrustRoot `json:"trustRoots"`
		Now        int64                `json:"now"`
		Channel    release.Channel      `json:"channel"`
	}
	if decodePackagingInput(input, 64<<10, &request) != nil || request.Now <= 0 ||
		(request.Channel != release.ChannelMain && request.Channel != release.ChannelBeta) ||
		len(request.TrustRoots) == 0 || len(request.TrustRoots) > 8 {
		return errors.New("release observation request is invalid")
	}
	roots := make([]release.TrustRoot, 0, len(request.TrustRoots))
	for _, value := range request.TrustRoots {
		key, err := base64.StdEncoding.Strict().DecodeString(value.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return errors.New("release observation public key is invalid")
		}
		roots = append(roots, release.TrustRoot{KeyID: value.KeyID, PublicKey: ed25519.PublicKey(key), State: value.State,
			NotBefore: time.Unix(value.NotBefore, 0), NotAfter: time.Unix(value.NotAfter, 0),
			MinSequence: value.MinSequence, MaxSequence: value.MaxSequence})
	}
	store, err := release.NewTrustStore(roots)
	if err != nil {
		return err
	}
	info, err := os.Lstat(candidate)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > release.MaxManifestBytes {
		return errors.New("release observation candidate is not a bounded regular file")
	}
	// #nosec G304 -- explicit operator packaging input, never an API or executable path.
	file, err := os.Open(candidate)
	if err != nil {
		return errors.New("release observation candidate is unavailable")
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("release observation candidate changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, release.MaxManifestBytes+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return errors.New("release observation candidate could not be read")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return errors.New("release observation candidate changed")
	}
	verified, err := release.Verify(raw, store, time.Unix(request.Now, 0), request.Channel, 0)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Schema   string           `json:"schema"`
		Digest   string           `json:"digest"`
		KeyID    string           `json:"keyId"`
		Manifest release.Manifest `json:"manifest"`
	}{"solovey.release-observation/v1", verified.Digest, verified.KeyID, verified.Manifest})
}

func decodePackagingInput(input io.Reader, limit int64, target any) error {
	raw, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil || int64(len(raw)) > limit || jsonvalue.StrictObject(raw) != nil {
		return errors.New("packaging JSON input is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return errors.New("packaging JSON fields are invalid")
	}
	return nil
}
