package singboxconfig

import "encoding/json"

func (d Document) DNS() (json.RawMessage, bool) { return d.section("dns") }

func (d Document) Route() (json.RawMessage, bool) { return d.section("route") }

func (d Document) section(name string) (json.RawMessage, bool) {
	raw, ok := d.sections[name]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), raw...), true
}
