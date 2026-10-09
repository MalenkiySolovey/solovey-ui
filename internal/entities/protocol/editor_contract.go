package protocol

import (
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
)

type EditorFacts struct {
	Fields      map[string][]string                 `json:"fields"`
	MemoryUnits map[string]uint64                   `json:"memoryUnits"`
	Snell       map[string]registry.SnellCapability `json:"snell"`
}

func EditorContract() EditorFacts {
	consumers := map[string]reflect.Type{"hysteria/in": reflect.TypeFor[option.HysteriaInboundOptions](), "hysteria/out": reflect.TypeFor[option.HysteriaOutboundOptions](), "hysteria2/in": reflect.TypeFor[option.Hysteria2InboundOptions](), "hysteria2/out": reflect.TypeFor[option.Hysteria2OutboundOptions](), "naive/in": reflect.TypeFor[option.NaiveInboundOptions](), "naive/out": reflect.TypeFor[option.NaiveOutboundOptions]()}
	facts := EditorFacts{Fields: map[string][]string{}, MemoryUnits: map[string]uint64{"B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30}}
	for consumer, typ := range consumers {
		facts.Fields[consumer] = jsonfields.Names(typ, false)
	}
	facts.Snell = map[string]registry.SnellCapability{"in": registry.SnellContract("inbounds"), "out": registry.SnellContract("outbounds")}
	return facts
}
