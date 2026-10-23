// This executable checks real Go build metadata; test binaries can omit module
// metadata. It opens no listener, database or runtime and emits only source facts.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

func main() {
	source := provider.Current()
	identity := coreinboundcontrol.ReadRuntimeIdentityV1()
	quic := coreinboundcontrol.ReadQUICBuildFeatureV1(identity)
	if source.State != provider.Verified || !coreinboundcontrol.RuntimeIdentityVerifiedV1(identity) ||
		identity.SingBoxModule != source.Module || identity.SingBoxVersion != source.ProviderVersion ||
		identity.SingBoxModuleSum != source.ModuleSum || identity.SingBoxSourceRevision != source.SourceRevision ||
		quic.State != coreinboundcontrol.BuildFeatureSupported || quic.SourceRevision != source.SourceRevision {
		fmt.Fprintln(os.Stderr, "provider source and runtime capability identity did not agree")
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Source  provider.Identity                        `json:"source"`
		Runtime coreinboundcontrol.CoreRuntimeIdentityV1 `json:"runtime"`
		QUIC    coreinboundcontrol.QUICBuildFeatureV1    `json:"quic"`
	}{source, identity, quic}); err != nil {
		fmt.Fprintln(os.Stderr, "cannot encode observed provider identity")
		os.Exit(1)
	}
}
