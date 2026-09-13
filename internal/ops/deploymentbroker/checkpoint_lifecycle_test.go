package deploymentbroker

import (
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestCheckpointDeploymentPrepareAuthorityLivesUntilTypedCheckpointRelease(t *testing.T) {
	prepare := completedMutationLifecycle(broker.VerbDeploymentPrepare)
	release := completedMutationLifecycle(broker.VerbDeploymentRelease)
	if !prepare.RetainUntilRelease || prepare.ReleasesVerb != "" {
		t.Fatalf("prepare lifecycle=%#v", prepare)
	}
	if release.RetainUntilRelease || release.ReleasesVerb != broker.VerbDeploymentPrepare {
		t.Fatalf("release lifecycle=%#v", release)
	}
}
