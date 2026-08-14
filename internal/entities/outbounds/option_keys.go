package outbounds

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

const maxOptionStripKeyOwners = 128
const maxOptionStripKeys = 1024

type registeredOptionStripKeys struct {
	keys  []string
	token uint64
}

var optionStripKeyOwners = struct {
	sync.RWMutex
	entries   map[string]registeredOptionStripKeys
	nextToken uint64
}{entries: map[string]registeredOptionStripKeys{}}

// RegisterOptionStripKeys registers component-owned presentation metadata
// accepted by the outbound edit boundary but never persisted as sing-box
// options. Persistence models deliberately know nothing about this registry.
func RegisterOptionStripKeys(owner string, keys ...string) func() {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		panic("outbound option strip-key owner is required")
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	if len(normalized) == 0 {
		panic("outbound option strip-key registration is empty")
	}
	sort.Strings(normalized)

	optionStripKeyOwners.Lock()
	defer optionStripKeyOwners.Unlock()
	if _, exists := optionStripKeyOwners.entries[owner]; exists {
		panic(fmt.Errorf("outbound option strip-key owner %q already registered", owner))
	}
	if len(optionStripKeyOwners.entries) >= maxOptionStripKeyOwners || optionStripKeyCountLocked()+len(normalized) > maxOptionStripKeys {
		panic("outbound option strip-key registry capacity exceeded")
	}
	optionStripKeyOwners.nextToken++
	token := optionStripKeyOwners.nextToken
	optionStripKeyOwners.entries[owner] = registeredOptionStripKeys{keys: normalized, token: token}
	return func() {
		optionStripKeyOwners.Lock()
		defer optionStripKeyOwners.Unlock()
		if current, ok := optionStripKeyOwners.entries[owner]; ok && current.token == token {
			delete(optionStripKeyOwners.entries, owner)
		}
	}
}

func decodeSaveOutbound(data json.RawMessage) (model.Outbound, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return model.Outbound{}, err
	}
	for _, key := range optionStripKeySnapshot() {
		delete(raw, key)
	}
	clean, err := json.Marshal(raw)
	if err != nil {
		return model.Outbound{}, err
	}
	var outbound model.Outbound
	if err := json.Unmarshal(clean, &outbound); err != nil {
		return model.Outbound{}, err
	}
	return outbound, nil
}

func optionStripKeySnapshot() []string {
	optionStripKeyOwners.RLock()
	defer optionStripKeyOwners.RUnlock()
	set := map[string]struct{}{}
	for _, registered := range optionStripKeyOwners.entries {
		for _, key := range registered.keys {
			set[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func optionStripKeyCountLocked() int {
	count := 0
	for _, registered := range optionStripKeyOwners.entries {
		count += len(registered.keys)
	}
	return count
}
