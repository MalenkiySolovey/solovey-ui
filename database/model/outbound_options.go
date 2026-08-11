package model

import (
	"strings"
	"sync"
)

type outboundOptionStripKey struct {
	token uint64
	key   string
}

const maxOutboundOptionStripKeys = 1024

var outboundOptionStripKeys = struct {
	sync.RWMutex
	next uint64
	keys []outboundOptionStripKey
}{}

func RegisterOutboundOptionStripKeys(owner string, keys ...string) func() {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return func() {}
	}
	registered := make([]outboundOptionStripKey, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		registered = append(registered, outboundOptionStripKey{key: key})
	}
	if len(registered) == 0 {
		return func() {}
	}

	outboundOptionStripKeys.Lock()
	if len(outboundOptionStripKeys.keys)+len(registered) > maxOutboundOptionStripKeys {
		outboundOptionStripKeys.Unlock()
		panic("outbound option strip-key registry capacity exceeded")
	}
	outboundOptionStripKeys.next++
	token := outboundOptionStripKeys.next
	for i := range registered {
		registered[i].token = token
	}
	outboundOptionStripKeys.keys = append(outboundOptionStripKeys.keys, registered...)
	outboundOptionStripKeys.Unlock()

	return func() {
		outboundOptionStripKeys.Lock()
		defer outboundOptionStripKeys.Unlock()
		dst := outboundOptionStripKeys.keys[:0]
		for _, key := range outboundOptionStripKeys.keys {
			if key.token != token {
				dst = append(dst, key)
			}
		}
		outboundOptionStripKeys.keys = dst
	}
}

func stripRegisteredOutboundOptionKeys(raw map[string]interface{}) {
	outboundOptionStripKeys.RLock()
	keys := append([]outboundOptionStripKey(nil), outboundOptionStripKeys.keys...)
	outboundOptionStripKeys.RUnlock()

	seen := make(map[string]struct{}, len(keys))
	for _, registered := range keys {
		if _, ok := seen[registered.key]; ok {
			continue
		}
		seen[registered.key] = struct{}{}
		delete(raw, registered.key)
	}
}
