package protocol

import (
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
)

type EditorFacts struct {
	Fields         map[string][]string                                    `json:"fields"`
	Enums          map[string]map[string][]string                         `json:"enums"`
	MemoryUnits    map[string]uint64                                      `json:"memoryUnits"`
	Snell          map[string]registry.SnellCapability                    `json:"snell"`
	Transports     map[string]TransportEditorFact                         `json:"transports"`
	CriticalFields map[string]map[string]registry.ProtocolFieldCapability `json:"criticalFields"`
}

func EditorContract() EditorFacts {
	consumers := map[string]reflect.Type{"hysteria/in": reflect.TypeFor[option.HysteriaInboundOptions](), "hysteria/out": reflect.TypeFor[option.HysteriaOutboundOptions](), "hysteria2/in": reflect.TypeFor[option.Hysteria2InboundOptions](), "hysteria2/out": reflect.TypeFor[option.Hysteria2OutboundOptions](), "naive/in": reflect.TypeFor[option.NaiveInboundOptions](), "naive/out": reflect.TypeFor[option.NaiveOutboundOptions](), "tuic/in": reflect.TypeFor[option.TUICInboundOptions](), "tuic/out": reflect.TypeFor[option.TUICOutboundOptions]()}
	facts := EditorFacts{Fields: map[string][]string{}, Enums: map[string]map[string][]string{}, MemoryUnits: map[string]uint64{"B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30}}
	for consumer, typ := range consumers {
		facts.Fields[consumer] = jsonfields.Names(typ, false)
		facts.Enums[consumer] = jsonfields.Enums(typ, false)
	}
	facts.Snell = map[string]registry.SnellCapability{"in": registry.SnellContract("inbounds"), "out": registry.SnellContract("outbounds")}
	facts.Transports = transportEditorFacts()
	facts.CriticalFields = registry.CriticalProtocolFields()
	return facts
}
