package ownedvalue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

func credentialFixture(t *testing.T) (secretref.Access, *secretbox.Box, string) {
	t.Helper()
	box, err := secretbox.NewRawKey(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	access := secretref.Access{Reference: secretref.NewReference("users", "client:42:mieru:password"), Actor: secretref.Actor{Component: "users"}, Operation: secretref.UseForOwner, IntentID: "runtime-config"}
	captured, err := Apply(access.Reference, "", json.RawMessage(`{"intent":"replace","value":"synthetic-credential"}`), box.EncryptString)
	if err != nil {
		t.Fatal(err)
	}
	return access, box, captured
}

func TestCredentialEnvelopeBindsOwnerRowAndField(t *testing.T) {
	access, box, captured := credentialFixture(t)
	for _, other := range []secretref.Reference{
		secretref.NewReference("inbounds", access.Reference.ID),
		secretref.NewReference("users", "client:43:mieru:password"),
		secretref.NewReference("users", "client:42:trusttunnel:password"),
	} {
		if _, err := box.DecryptBytes(captured, AAD(other)); err == nil {
			t.Fatal("credential transferred across owner, row or field")
		}
	}
	view := View(access, captured)
	encoded, err := json.Marshal(view)
	if err != nil || !view.Present || len(view.RotationRevision) != 64 || bytes.Contains(encoded, []byte("synthetic-credential")) || bytes.Contains(encoded, []byte(captured)) {
		t.Fatal("presence leaked material or lost ciphertext revision")
	}
	legacy := View(access, "legacy-plaintext")
	if !legacy.Present || legacy.RotationRevision != "" {
		t.Fatal("plaintext was fingerprinted as a rotation revision")
	}
}

func TestExplicitMutationPreservesOnlyExactCapturedReference(t *testing.T) {
	access, box, captured := credentialFixture(t)
	view := View(access, captured)
	unchanged := map[string]any{"intent": "unchanged", "reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision}
	raw, _ := json.Marshal(unchanged)
	actual, err := Apply(access.Reference, captured, raw, nil)
	if err != nil || actual != captured {
		t.Fatal("unchanged resealed or lost the captured credential")
	}
	rotated, err := Apply(access.Reference, captured, json.RawMessage(`{"intent":"replace","value":"replacement-fixture"}`), box.EncryptString)
	if err != nil || rotated == captured {
		t.Fatal("replacement did not create new authenticated material")
	}
	if _, err := Apply(access.Reference, rotated, raw, nil); !errors.Is(err, secretref.ErrDenied) {
		t.Fatal("stale unchanged editor was admitted")
	}
	unchanged["reference"] = secretref.NewReference("users", "client:43:mieru:password")
	raw, _ = json.Marshal(unchanged)
	if _, err := Apply(access.Reference, captured, raw, nil); !errors.Is(err, secretref.ErrDenied) {
		t.Fatal("another row's reference was admitted")
	}
	if actual, err := Apply(access.Reference, captured, json.RawMessage(`{"intent":"clear"}`), nil); err != nil || actual != "" {
		t.Fatal("explicit clear failed")
	}
	for _, raw := range []string{
		`null`, `"stored"`, `{}`, `{"intent":"replace","value":""}`,
		`{"intent":"replace","value":"sbox:v1:untrusted"}`,
		`{"intent":"replace","value":"first","value":"second"}`,
		`{"intent":"clear","value":"synthetic-credential"}`,
		`{"intent":"clear","unknown":"synthetic-credential"}`,
		`{"intent":"unchanged"}`, `{"intent":"clear"} {"intent":"replace"}`,
	} {
		if _, err := Apply(access.Reference, captured, json.RawMessage(raw), box.EncryptString); !errors.Is(err, secretref.ErrDenied) {
			t.Fatal("ambiguous or untrusted credential mutation was accepted")
		}
	}
	oversized, _ := json.Marshal(map[string]any{"intent": "replace", "value": strings.Repeat("x", MaxValueBytes+1)})
	if _, err := Apply(access.Reference, captured, oversized, box.EncryptString); !errors.Is(err, secretref.ErrDenied) {
		t.Fatal("unbounded credential accepted")
	}
}

func TestAccessAuditsBeforeUseAndWipesBorrowedValue(t *testing.T) {
	access, box, captured := credentialFixture(t)
	var borrowed []byte
	admitted, consumed := false, false
	view, err := Access(context.Background(), access, captured, func() ([]byte, error) {
		var err error
		borrowed, err = box.DecryptBytes(captured, AAD(access.Reference))
		return borrowed, err
	}, func(view secretref.View) error {
		admitted = true
		if consumed || view.RotationRevision != View(access, captured).RotationRevision {
			t.Fatal("audit no longer refers to the admitted ciphertext")
		}
		return nil
	}, func(value []byte) error {
		consumed = true
		if !admitted || string(value) != "synthetic-credential" {
			t.Fatal("owner received a different or unaudited value")
		}
		return nil
	})
	if err != nil || !view.Present || !consumed || !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("access failed or retained borrowed plaintext")
	}
	access.Operation = secretref.Presence
	if _, err := Access(context.Background(), access, captured, func() ([]byte, error) { t.Fatal("presence decrypted"); return nil, nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAccessFailsClosedOnAuthorityAuditCancellationAndConsumerErrors(t *testing.T) {
	access, _, captured := credentialFixture(t)
	for _, failure := range []string{"decode", "audit", "denied", "consumer", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			borrowed := []byte("synthetic-credential")
			consumed := false
			_, err := Access(ctx, access, captured, func() ([]byte, error) {
				if failure == "decode" {
					return borrowed, errors.New("synthetic-credential")
				}
				return borrowed, nil
			}, func(secretref.View) error {
				switch failure {
				case "audit":
					return errors.New("synthetic-credential")
				case "denied":
					return secretref.ErrDenied
				case "cancel":
					cancel()
				}
				return nil
			}, func([]byte) error { consumed = true; return errors.New("synthetic-credential") })
			if err == nil || strings.Contains(err.Error(), "synthetic-credential") || !bytes.Equal(borrowed, make([]byte, len(borrowed))) || consumed != (failure == "consumer") {
				t.Fatal("failure exposed material, skipped wiping or reached an unauthorized consumer")
			}
		})
	}
	access.Actor.Component = "other-owner"
	if _, err := Access(context.Background(), access, captured, nil, nil, func([]byte) error { t.Fatal("wrong owner consumed"); return nil }); !errors.Is(err, secretref.ErrDenied) {
		t.Fatal("wrong actor admitted")
	}
}
