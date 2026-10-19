package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeNFTExecutor struct {
	support          NFTSupport
	state            []byte
	present          bool
	checkErr         error
	listErr          error
	applyErr         error
	deleteErr        error
	deleteCalls      int
	verifyMismatch   bool
	rollbackMismatch bool
	checks           int
	applies          int
}

func testSemanticSHA(t *testing.T, data []byte) string {
	t.Helper()
	value, err := ManagedSemanticSHA256(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testTimedMembershipSHA(t *testing.T, data []byte) string {
	t.Helper()
	value, err := ManagedTimedMembershipSHA256(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (f *fakeNFTExecutor) Detect(context.Context) NFTSupport { return f.support }
func (f *fakeNFTExecutor) CheckManagedFile(context.Context, string) error {
	f.checks++
	return f.checkErr
}
func (f *fakeNFTExecutor) ListManagedTable(context.Context) ([]byte, bool, error) {
	if f.listErr != nil {
		return nil, false, f.listErr
	}
	return append([]byte(nil), f.state...), f.present, nil
}
func (f *fakeNFTExecutor) ApplyManagedFile(_ context.Context, path string) error {
	f.applies++
	if f.applyErr != nil {
		return f.applyErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	index := strings.LastIndex(text, "table inet solovey_protection {")
	if index < 0 {
		f.state, f.present = nil, false
		return nil
	}
	f.state, f.present = append([]byte(nil), data[index:]...), true
	if f.verifyMismatch {
		f.state = []byte(strings.Replace(string(f.state), "\"\n", "-forged\"\n", 1))
	}
	if f.rollbackMismatch && f.applies > 1 {
		f.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + strings.Repeat("f", 64) + "\"\n}\n")
	}
	return nil
}

func (f *fakeNFTExecutor) DeleteManagedTable(context.Context) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.state, f.present = nil, false
	return nil
}

func writeTestManagedFile(t *testing.T, root ManagedRoot, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root.Path(), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNFTCapabilityUnknownNeverReportsSuccess(t *testing.T) {
	root := testManagedRoot(t)
	unknown := newContractEngineWithExecutor(root, nil).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "capabilities", InstanceID: "instance"}, Operation: OperationCapabilities, Capabilities: &CapabilitiesRequest{}})
	if !unknown.OK || unknown.Capabilities.NFT.PlatformKnown || CapabilityAvailable(unknown.Capabilities, OperationNFTApply) {
		t.Fatalf("unknown capability became supported: %#v", unknown)
	}
}
