package nftables

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The preserved adapter regressions use this test-only envelope to invoke the
// actual Engine methods. Authentication/lock/protocol tests remain in helper.
const (
	ProtocolVersion      = 1
	OperationNFTValidate = "nft.validate"
	OperationNFTApply    = "nft.managed_table.apply"
	OperationNFTRollback = "nft.managed_table.rollback"
	CodeValidationFailed = "validation_failed"
)

type Request struct {
	ProtocolVersion int
	Correlation     Correlation
	Operation       string
	NFTValidate     *NFTValidateRequest
	NFTApply        *NFTApplyRequest
	NFTRollback     *NFTRollbackRequest
}

type Response struct {
	OK     bool
	Code   string
	Reason string
	NFT    *NFTResult
}

type fixtureEngine struct{ Engine }

func newContractEngineWithExecutor(root ManagedRoot, executor NFTExecutor) fixtureEngine {
	return fixtureEngine{NewEngineWithExecutor(root, executor, time.Now)}
}

func (e fixtureEngine) Handle(request Request) Response {
	var result *NFTResult
	var err error
	switch request.Operation {
	case OperationNFTValidate:
		result, err = e.Validate(context.Background(), request.Correlation, *request.NFTValidate)
	case OperationNFTApply:
		result, err = e.Apply(context.Background(), request.Correlation, *request.NFTApply)
	case OperationNFTRollback:
		result, err = e.Rollback(context.Background(), request.Correlation, *request.NFTRollback)
	default:
		err = errors.New("fixture operation unavailable")
	}
	if err != nil {
		return Response{Code: CodeValidationFailed, Reason: err.Error()}
	}
	return Response{OK: true, NFT: result}
}

type fixtureRoot string

func (r fixtureRoot) Path() string { return string(r) }
func (r fixtureRoot) Resolve(relative string, exists bool) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") || strings.ContainsRune(relative, 0) {
		return "", errors.New("fixture path forbidden")
	}
	path := filepath.Join(string(r), filepath.FromSlash(relative))
	if exists {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func testManagedRoot(t *testing.T) ManagedRoot {
	t.Helper()
	return fixtureRoot(t.TempDir())
}
