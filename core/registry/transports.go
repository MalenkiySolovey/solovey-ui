package registry

import (
	"encoding/json"
	"sync"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

var transportFacts = sync.OnceValue(func() []Fact {
	var result []Fact
	for _, kind := range []string{"http", "ws", "quic", "grpc", "httpupgrade", "xhttp", "mkcp"} {
		probe := map[string]any{"type": kind}
		if kind == "xhttp" {
			probe["x_padding_bytes"] = "100-1000"
		}
		raw, _ := json.Marshal(probe)
		var native option.V2RayTransportOptions
		registered := sbjson.Unmarshal(raw, &native) == nil
		fact := Fact{Category: "transports", Type: kind, Known: true, Registered: registered, Compiled: registered, SupportedByProduct: true}
		if !registered {
			fact.Reason = "TRANSPORT_SCHEMA_UNAVAILABLE"
		}
		if kind == "quic" {
			fact.BuildTag = "with_quic"
			fact.Compiled = registered && C.WithQUIC
			if registered && !fact.Compiled {
				fact.Reason = "KNOWN_BUT_NOT_COMPILED"
			}
		}
		// gRPC has a native lite implementation without with_grpc. XHTTP's
		// native HTTP factory is unconditional in the source that declares it.
		result = append(result, fact)
	}
	return result
})

// Nested transport schemas have no entity registry or independent DB owner.
// Keep their compiled facts here and project them through the protocol editor.
func TransportFacts() []Fact { return append([]Fact(nil), transportFacts()...) }

func ResolveTransport(kind string) Fact {
	for _, fact := range transportFacts() {
		if fact.Type == kind {
			return fact
		}
	}
	return Fact{Category: "transports", Type: kind, Reason: "UNKNOWN_TRANSPORT_TYPE"}
}
