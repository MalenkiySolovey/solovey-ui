package backendfronting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	fronting "github.com/MalenkiySolovey/solovey-component-server-protection/service/fronting"
	helper "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
)

type capabilityReader struct {
	response helper.Response
	calls    int
}

func (r *capabilityReader) Execute(_ context.Context, request helper.Request) (helper.Response, error) {
	r.calls++
	if request.Operation != helper.OperationCapabilities {
		return helper.Response{}, errors.New("unexpected mutation")
	}
	return r.response, nil
}

func TestInspectionAuthorityRejectsUnknownForeignAndStaleInputs(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "nginx")
	controlled := filepath.Join(root, "loader.conf")
	for _, name := range []string{binary, controlled} {
		if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, condition := range []string{"current", "missing-owner", "unknown", "failed-response", "foreign-binary", "foreign-root", "foreign-config", "extra-candidate", "stale-capability", "stale-active"} {
		t.Run(condition, func(t *testing.T) {
			capabilities := &helper.CapabilitiesResult{Revision: "current-capability", HelperVersion: "1.5.1", ContractVersion: "1.5", Nginx: helper.NginxSupport{PlatformKnown: true, Linux: true, Available: true, Binary: helper.BinaryIdentity{TargetPath: binary}, ManagedRoot: root, ControlledConfig: controlled, ActiveRevision: "current-active"}}
			reader := &capabilityReader{response: helper.Response{OK: true, Capabilities: capabilities}}
			owner := RuntimeInspector{helper: reader, instanceID: "test-instance"}
			config := fronting.NginxRuntimeInspectionConfigV2{CandidatePaths: []string{binary}, AllowedExecutableRoots: []string{root}, ManagedRootPath: root, ControlledConfigPath: controlled,
				InstallationClass: fronting.NginxInstallationManaged, HelperContractRevision: capabilities.Revision, ActiveManagedRevision: capabilities.Nginx.ActiveRevision,
				HelperProtocolVersion: helper.ProtocolVersion, HelperVersion: capabilities.HelperVersion, HelperContractVersion: capabilities.ContractVersion,
				ObservedAt: time.Now().UTC()}
			switch condition {
			case "missing-owner":
				owner = RuntimeInspector{}
			case "unknown":
				capabilities.Nginx.Available = false
			case "failed-response":
				reader.response.OK = false
			case "foreign-binary":
				config.CandidatePaths = []string{filepath.Join(t.TempDir(), "foreign")}
			case "foreign-root":
				config.AllowedExecutableRoots = []string{filepath.Dir(root)}
			case "foreign-config":
				config.ControlledConfigPath = filepath.Join(t.TempDir(), "foreign.conf")
			case "extra-candidate":
				config.CandidatePaths = append(config.CandidatePaths, controlled)
			case "stale-capability":
				config.HelperContractRevision = "previous-capability"
			case "stale-active":
				config.ActiveManagedRevision = "previous-active"
			}
			identity, err := owner.Inspect(context.Background(), config, fronting.NginxVersionObservationV2{Version: "1.27.4", ConfigureArguments: []string{"--with-stream"}})
			if condition == "current" {
				if err != nil || identity.ExecutableDigest == "" || identity.Version != "1.27.4" {
					t.Fatalf("current owner observation lost: %#v %v", identity, err)
				}
			} else if err == nil || identity.CanonicalRuntimeIdentityRevision != "" {
				t.Fatalf("%s admitted an identity: %#v %v", condition, identity, err)
			}
		})
	}
}

type versionReaderFunc func(context.Context, string) (fronting.NginxVersionObservationV2, error)

func (f versionReaderFunc) ReadNginxVersion(ctx context.Context, path string) (fronting.NginxVersionObservationV2, error) {
	return f(ctx, path)
}

func TestInspectorCancellationCannotPublishAnIdentity(t *testing.T) {
	for _, cancelDuringRead := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		root := t.TempDir()
		binary := filepath.Join(root, "nginx")
		if err := os.WriteFile(binary, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reader := versionReaderFunc(func(context.Context, string) (fronting.NginxVersionObservationV2, error) {
			calls++
			cancel()
			return fronting.NginxVersionObservationV2{Version: "1.27.4"}, nil
		})
		if !cancelDuringRead {
			cancel()
		}
		identity, err := (NginxRuntimeInspectorV2{Config: fronting.NginxRuntimeInspectionConfigV2{CandidatePaths: []string{binary}, AllowedExecutableRoots: []string{root}}, Reader: reader}).Inspect(ctx)
		cancel()
		if !errors.Is(err, context.Canceled) || identity.CanonicalRuntimeIdentityRevision != "" || calls != map[bool]int{false: 0, true: 1}[cancelDuringRead] {
			t.Fatalf("canceled observation escaped: calls=%d identity=%#v err=%v", calls, identity, err)
		}
	}
}
