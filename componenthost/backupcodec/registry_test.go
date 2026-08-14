package backupcodec

import (
	"bytes"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegistryRejectsDuplicateAndUnregistersExactlyOnce(t *testing.T) {
	codec := ExportCodec{
		Selected: func(*gin.Context) bool { return true },
		Encode:   func(ExportContext) (ExportResult, error) { return ExportResult{}, nil },
	}
	unregister, err := RegisterExport("owner", codec)
	if err != nil {
		t.Fatal(err)
	}
	unregister()
	unregister()
	replacement, err := RegisterExport("owner", codec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replacement)
	if _, err := RegisterExport("owner", codec); err == nil {
		t.Fatal("duplicate export codec registration was accepted")
	}
}

func TestImportPassphraseFieldsFollowCodecLifecycle(t *testing.T) {
	newCodec := func(fields ...string) ImportCodec {
		return ImportCodec{
			HeaderBytes:      1,
			PassphraseFields: fields,
			Match:            func(header []byte) bool { return bytes.Equal(header, []byte("x")) },
			Decode:           func(ImportContext) ([]byte, error) { return nil, nil },
		}
	}
	unregisterA, err := RegisterImport("a", newCodec("ownerPassphrase", "sharedPassphrase"))
	if err != nil {
		t.Fatal(err)
	}
	unregisterB, err := RegisterImport("b", newCodec("sharedPassphrase", "secondPassphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ImportPassphraseFields(), []string{"ownerPassphrase", "secondPassphrase", "sharedPassphrase"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("passphrase fields = %v, want %v", got, want)
	}
	unregisterA()
	if got, want := ImportPassphraseFields(), []string{"secondPassphrase", "sharedPassphrase"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("passphrase fields after unregister = %v, want %v", got, want)
	}
	unregisterB()
}

func TestImportPassphraseFieldsRejectInvalidDeclarations(t *testing.T) {
	for _, fields := range [][]string{{"not-valid"}, {"duplicate", "duplicate"}} {
		if _, err := RegisterImport("invalid", ImportCodec{
			HeaderBytes:      1,
			PassphraseFields: fields,
			Match:            func([]byte) bool { return true },
			Decode:           func(ImportContext) ([]byte, error) { return nil, nil },
		}); err == nil {
			t.Fatalf("invalid passphrase fields %v were accepted", fields)
		}
	}
}

func TestExportRequestDetectionIsBoundedToKnownSelectors(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/backup?backupEncryption=owner", nil)
	if !ExportRequested(context) {
		t.Fatal("explicit codec selection was not detected")
	}
	context.Request = httptest.NewRequest("GET", "/backup?backupEncryption=none", nil)
	if ExportRequested(context) {
		t.Fatal("plain selection was treated as codec request")
	}
}

func TestSelectionFailsClosedOnAmbiguousOrPanickingCodec(t *testing.T) {
	codec := func(selected func(*gin.Context) bool) ExportCodec {
		return ExportCodec{Selected: selected, Encode: func(ExportContext) (ExportResult, error) { return ExportResult{}, nil }}
	}
	unregisterOne, err := RegisterExport("one", codec(func(*gin.Context) bool { return true }))
	if err != nil {
		t.Fatal(err)
	}
	unregisterTwo, err := RegisterExport("two", codec(func(*gin.Context) bool { return true }))
	if err != nil {
		unregisterOne()
		t.Fatal(err)
	}
	if _, _, _, err := SelectedExport(nil); err == nil {
		t.Fatal("ambiguous export codec authority was selected")
	}
	unregisterTwo()
	unregisterOne()
	unregisterPanic, err := RegisterExport("panic", codec(func(*gin.Context) bool { panic("secret") }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregisterPanic)
	if _, _, _, err := SelectedExport(nil); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panicking selector was not contained: %v", err)
	}
}

func TestStaleCodecCleanupDoesNotRemoveReplacement(t *testing.T) {
	codec := ExportCodec{Selected: func(*gin.Context) bool { return true }, Encode: func(ExportContext) (ExportResult, error) { return ExportResult{}, nil }}
	cleanupOld, err := RegisterExport("owner", codec)
	if err != nil {
		t.Fatal(err)
	}
	cleanupOld()
	cleanupNew, err := RegisterExport("owner", codec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanupNew)
	cleanupOld()
	if _, _, ok, err := SelectedExport(nil); err != nil || !ok {
		t.Fatal("stale cleanup removed a newer codec registration")
	}
}
