package backupcodec

import (
	"bytes"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegistryRejectsDuplicateAndUnregistersExactlyOnce(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	codec := ExportCodec{
		Selected: func(*gin.Context) bool { return true },
		Encode:   func(ExportContext) (ExportResult, error) { return ExportResult{}, nil },
	}
	unregister := RegisterExport("owner", codec)
	unregister()
	unregister()
	replacement := RegisterExport("owner", codec)
	t.Cleanup(replacement)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("duplicate export codec registration did not panic")
			}
		}()
		RegisterExport("owner", codec)
	}()
}

func TestImportPassphraseFieldsFollowCodecLifecycle(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	newCodec := func(fields ...string) ImportCodec {
		return ImportCodec{
			HeaderBytes:      1,
			PassphraseFields: fields,
			Match:            func(header []byte) bool { return bytes.Equal(header, []byte("x")) },
			Decode:           func(ImportContext) ([]byte, error) { return nil, nil },
		}
	}
	unregisterA := RegisterImport("a", newCodec("ownerPassphrase", "sharedPassphrase"))
	unregisterB := RegisterImport("b", newCodec("sharedPassphrase", "secondPassphrase"))
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
	ResetForTest()
	t.Cleanup(ResetForTest)
	for _, fields := range [][]string{{"not-valid"}, {"duplicate", "duplicate"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("invalid passphrase fields %v did not panic", fields)
				}
			}()
			RegisterImport("invalid", ImportCodec{
				HeaderBytes:      1,
				PassphraseFields: fields,
				Match:            func([]byte) bool { return true },
				Decode:           func(ImportContext) ([]byte, error) { return nil, nil },
			})
		}()
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
