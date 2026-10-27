package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"filippo.io/edwards25519"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// Sudoku master/split keys describe client delivery. The serving protocol has
// one public key; distinct splits do not create independently revocable users.
func sudokuMaster(value string) (*edwards25519.Scalar, error) {
	encoded, err := hex.DecodeString(strings.TrimSpace(value))
	defer common.WipeBytes(encoded)
	if err != nil || len(encoded) != 32 {
		return nil, ErrInvalid
	}
	master, err := edwards25519.NewScalar().SetCanonicalBytes(encoded)
	if err != nil || master.Equal(edwards25519.NewScalar()) == 1 {
		return nil, ErrInvalid
	}
	return master, nil
}

func SudokuPublicKey(value string) (string, error) {
	master, err := sudokuMaster(value)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(edwards25519.NewIdentityPoint().ScalarBaseMult(master).Bytes()), nil
}

func GenerateSudokuMaster() (string, error) {
	var seed [64]byte
	defer common.WipeBytes(seed[:])
	for {
		if _, err := rand.Read(seed[:]); err != nil {
			return "", ErrInvalid
		}
		master, err := edwards25519.NewScalar().SetUniformBytes(seed[:])
		if err != nil {
			return "", ErrInvalid
		}
		if master.Equal(edwards25519.NewScalar()) != 1 {
			return hex.EncodeToString(master.Bytes()), nil
		}
	}
}

func NormalizeSudokuSplit(value string) (string, error) {
	encoded, err := hex.DecodeString(strings.TrimSpace(value))
	defer common.WipeBytes(encoded)
	if err != nil || len(encoded) != 64 {
		return "", ErrInvalid
	}
	left, err := edwards25519.NewScalar().SetCanonicalBytes(encoded[:32])
	if err != nil {
		return "", ErrInvalid
	}
	right, err := edwards25519.NewScalar().SetCanonicalBytes(encoded[32:])
	if err != nil || edwards25519.NewScalar().Add(left, right).Equal(edwards25519.NewScalar()) == 1 {
		return "", ErrInvalid
	}
	return hex.EncodeToString(encoded), nil
}

func SudokuSplitMatches(value, masterValue string) bool {
	normalized, err := NormalizeSudokuSplit(value)
	if err != nil {
		return false
	}
	master, err := sudokuMaster(masterValue)
	if err != nil {
		return false
	}
	encoded, _ := hex.DecodeString(normalized)
	defer common.WipeBytes(encoded)
	left, _ := edwards25519.NewScalar().SetCanonicalBytes(encoded[:32])
	right, _ := edwards25519.NewScalar().SetCanonicalBytes(encoded[32:])
	return edwards25519.NewScalar().Add(left, right).Equal(master) == 1
}

// SudokuSplitPublicKey verifies a delivered split against the serving identity
// without retaining or requiring the private master after delivery.
func SudokuSplitPublicKey(value string) (string, error) {
	normalized, err := NormalizeSudokuSplit(value)
	if err != nil {
		return "", err
	}
	encoded, _ := hex.DecodeString(normalized)
	defer common.WipeBytes(encoded)
	left, _ := edwards25519.NewScalar().SetCanonicalBytes(encoded[:32])
	right, _ := edwards25519.NewScalar().SetCanonicalBytes(encoded[32:])
	master := edwards25519.NewScalar().Add(left, right)
	return hex.EncodeToString(edwards25519.NewIdentityPoint().ScalarBaseMult(master).Bytes()), nil
}

func SplitSudokuMaster(value string) (string, error) {
	master, err := sudokuMaster(value)
	if err != nil {
		return "", err
	}
	var seed [64]byte
	defer common.WipeBytes(seed[:])
	if _, err := rand.Read(seed[:]); err != nil {
		return "", ErrInvalid
	}
	left, err := edwards25519.NewScalar().SetUniformBytes(seed[:])
	if err != nil {
		return "", ErrInvalid
	}
	right := edwards25519.NewScalar().Subtract(master, left)
	encoded := append(left.Bytes(), right.Bytes()...)
	defer common.WipeBytes(encoded)
	return hex.EncodeToString(encoded), nil
}

// UseSudokuMaster borrows the Inbounds-owned master only to derive its public
// key or prepare Users-owned delivery keys in the caller's admitted transaction.
func UseSudokuMaster(ctx context.Context, db *gorm.DB, inbound model.Inbound, intentID string, use func([]byte) error) error {
	if inbound.Type != "sudoku" || use == nil || jsonvalue.StrictObject(inbound.Options) != nil {
		return ErrInvalid
	}
	var options struct {
		MasterKey string `json:"master_key"`
	}
	if json.Unmarshal(inbound.Options, &options) != nil || options.MasterKey == "" {
		return secretref.ErrUnavailable
	}
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "master_key")
	if err != nil {
		return err
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "inbounds"}, Operation: secretref.UseForOwner, IntentID: intentID}
	_, err = ownerdb.Access(ctx, db, request, options.MasterKey, use)
	return err
}

// SudokuKeyBag uses canonical numeric owner IDs. The whole JSON bundle is one
// fixed users/row/sudoku.keys SecretRef, including preserved unassigned keys.
func SudokuKeyBag(value string) (map[string]string, error) {
	if len(value) > 1<<20 || jsonvalue.StrictObject([]byte(value)) != nil {
		return nil, ErrInvalid
	}
	var keys map[string]string
	if json.Unmarshal([]byte(value), &keys) != nil || keys == nil || len(keys) > 1024 {
		return nil, ErrInvalid
	}
	for id, value := range keys {
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != id {
			return nil, ErrInvalid
		}
		key, err := NormalizeSudokuSplit(value)
		if err != nil {
			return nil, ErrInvalid
		}
		keys[id] = key
	}
	return keys, nil
}
