package local

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"
)

type ClientOutboundContributionContext struct {
	DB       *gorm.DB
	RawLinks json.RawMessage
	Target   string
}

type ClientOutboundContributor func(ClientOutboundContributionContext, *OutboundSet) error

var clientOutboundContributors = struct {
	sync.RWMutex
	entries map[string]ClientOutboundContributor
	version atomic.Uint64
}{
	entries: map[string]ClientOutboundContributor{},
}

func RegisterClientOutboundContributor(name string, fn ClientOutboundContributor) func() {
	if name == "" {
		panic("client outbound contributor name is required")
	}
	if fn == nil {
		panic(fmt.Errorf("client outbound contributor %q is nil", name))
	}

	clientOutboundContributors.Lock()
	if _, exists := clientOutboundContributors.entries[name]; exists {
		clientOutboundContributors.Unlock()
		panic(fmt.Errorf("client outbound contributor %q already registered", name))
	}
	clientOutboundContributors.entries[name] = fn
	clientOutboundContributors.version.Add(1)
	clientOutboundContributors.Unlock()

	return func() {
		clientOutboundContributors.Lock()
		if _, exists := clientOutboundContributors.entries[name]; exists {
			delete(clientOutboundContributors.entries, name)
			clientOutboundContributors.version.Add(1)
		}
		clientOutboundContributors.Unlock()
	}
}

func AppendClientOutboundContributions(ctx ClientOutboundContributionContext, set *OutboundSet) error {
	if set == nil {
		return nil
	}
	contributors := clientOutboundContributorSnapshot()
	for _, contributor := range contributors {
		if err := contributor.fn(ctx, set); err != nil {
			return fmt.Errorf("client outbound contributor %q failed: %w", contributor.name, err)
		}
	}
	return nil
}

func ClientOutboundContributorsVersion() uint64 {
	return clientOutboundContributors.version.Load()
}

func ResetClientOutboundContributorsForTest() {
	clientOutboundContributors.Lock()
	clientOutboundContributors.entries = map[string]ClientOutboundContributor{}
	clientOutboundContributors.version.Add(1)
	clientOutboundContributors.Unlock()
}

type clientOutboundContributorEntry struct {
	name string
	fn   ClientOutboundContributor
}

func clientOutboundContributorSnapshot() []clientOutboundContributorEntry {
	clientOutboundContributors.RLock()
	entries := make([]clientOutboundContributorEntry, 0, len(clientOutboundContributors.entries))
	for name, fn := range clientOutboundContributors.entries {
		entries = append(entries, clientOutboundContributorEntry{name: name, fn: fn})
	}
	clientOutboundContributors.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
