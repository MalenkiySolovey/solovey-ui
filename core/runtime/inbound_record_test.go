package runtime

import (
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestInboundRuntimeRecordBindsCompleteTypedOptionsAndGeneration(t *testing.T) {
	base := option.Inbound{
		Type: "http", Tag: "tracked-inbound",
		Options: &option.HTTPMixedInboundOptions{ListenOptions: option.ListenOptions{ListenPort: 18080}},
	}
	ctx := NewCore().GetCtx()
	first, ok := inboundRuntimeRecord(ctx, base, 7)
	if !ok || first.Tag != base.Tag || first.Type != base.Type || first.OptionsDigest == "" || first.ManagerGeneration != 7 {
		t.Fatalf("record = %#v, ok=%v", first, ok)
	}
	base.Options.(*option.HTTPMixedInboundOptions).ListenPort++
	second, ok := inboundRuntimeRecord(ctx, base, 8)
	if !ok || second.OptionsDigest == first.OptionsDigest || second.ManagerGeneration != 8 {
		t.Fatalf("changed record = %#v, ok=%v", second, ok)
	}
}
