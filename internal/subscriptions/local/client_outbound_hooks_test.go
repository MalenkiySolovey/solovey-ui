package local

import "testing"

func TestClientOutboundContributorStaleCleanupPreservesNewRegistration(t *testing.T) {
	oldCleanup := RegisterClientOutboundContributor("test.generation", func(ClientOutboundContributionContext, *OutboundSet) error { return nil })
	oldCleanup()
	newCleanup := RegisterClientOutboundContributor("test.generation", func(ClientOutboundContributionContext, *OutboundSet) error { return nil })
	t.Cleanup(newCleanup)

	oldCleanup()
	if entries := clientOutboundContributorSnapshot(); len(entries) != 1 {
		t.Fatalf("stale cleanup changed current registry: %#v", entries)
	}
}
