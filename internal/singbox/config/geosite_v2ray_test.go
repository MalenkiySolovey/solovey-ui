package singboxconfig

import (
	"bytes"
	"math"
	"testing"

	"github.com/sagernet/sing-box/common/srs"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestGeositeDomainTypeCheckedBeforeNarrowing(t *testing.T) {
	for typ := uint64(0); typ <= 3; typ++ {
		wire := geositeDomainTypeFixture(typ)
		domain, err := parseV2RayDomain(wire)
		if err != nil || domain.Type != int32(typ) || domain.Value != "fixture.example" {
			t.Fatalf("supported enum %d rejected: %v", typ, err)
		}
	}
	for _, typ := range []uint64{4, math.MaxInt32, 1 << 31, 1 << 32, 1<<32 + 1, 1<<32 + 2, 1<<32 + 3, math.MaxUint64} {
		if _, err := parseV2RayDomain(geositeDomainTypeFixture(typ)); err == nil {
			t.Fatalf("unsupported/overflow enum %d accepted", typ)
		}
		if _, err := compileGeositeRuSmartDirect(geositeCategoryFixture(geositeDomainTypeFixture(typ))); err == nil {
			t.Fatalf("RU-smart conversion accepted enum %d", typ)
		}
	}
	for _, wire := range [][]byte{{0x08, 0x80}, {0x08, 0x80, 0x80}, {0x08, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}} {
		if _, err := parseV2RayDomain(wire); err == nil {
			t.Fatal("truncated/overflow varint accepted")
		}
	}
}

func TestGeositeSupportedTypesRetainRuleSetMatchers(t *testing.T) {
	for typ := uint64(0); typ <= 3; typ++ {
		compiled, err := compileGeositeRuSmartDirect(geositeCategoryFixture(geositeDomainTypeFixture(typ)))
		if err != nil {
			t.Fatalf("supported type %d conversion: %v", typ, err)
		}
		decoded, err := srs.Read(bytes.NewReader(compiled), true)
		if err != nil || len(decoded.Options.Rules) != 1 {
			t.Fatalf("supported type %d rule-set roundtrip: %v", typ, err)
		}
		rule := decoded.Options.Rules[0].DefaultOptions
		matchers := [][]string{rule.DomainKeyword, rule.DomainRegex, rule.DomainSuffix, rule.Domain}
		for index, values := range matchers {
			if uint64(index) == typ {
				if len(values) != 1 || values[0] != "fixture.example" {
					t.Fatalf("type %d lost its matcher", typ)
				}
			} else if len(values) != 0 {
				t.Fatalf("type %d changed matcher semantics", typ)
			}
		}
	}
}

func geositeDomainTypeFixture(typ uint64) []byte {
	wire := protowire.AppendTag(nil, 1, protowire.VarintType)
	wire = protowire.AppendVarint(wire, typ)
	wire = protowire.AppendTag(wire, 2, protowire.BytesType)
	return protowire.AppendString(wire, "fixture.example")
}

func geositeCategoryFixture(domain []byte) []byte {
	site := protowire.AppendTag(nil, 1, protowire.BytesType)
	site = protowire.AppendString(site, managedRuSmartCategory)
	site = protowire.AppendTag(site, 2, protowire.BytesType)
	site = protowire.AppendBytes(site, domain)
	list := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(list, site)
}
