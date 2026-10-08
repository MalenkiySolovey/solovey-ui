package box

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

type candidateInbound struct {
	adapter.Inbound
	closes int
}

func (i *candidateInbound) Close() error { i.closes++; return nil }

type candidateRegistry struct {
	adapter.InboundRegistry
	candidate adapter.Inbound
}

func (r candidateRegistry) Create(context.Context, adapter.Router, log.ContextLogger, string, string, any) (adapter.Inbound, error) {
	return r.candidate, nil
}

func TestUnpublishedHotCandidateCleanupPreservesActualInbound(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		candidate := &candidateInbound{}
		registry := constructionRegistry{candidateRegistry{candidate: candidate}}
		ctx, finish := BeginInboundConstruction(t.Context(), "in")
		actual, err := registry.Create(ctx, nil, log.NewNOPFactory().Logger(), "in", "fixture", nil)
		if err != nil || actual != candidate {
			t.Fatal("construction adapter changed inbound identity")
		}
		var failure error
		if !accepted {
			failure = errors.New("start failed")
		}
		if !errors.Is(finish(failure), failure) {
			t.Fatal("failure lost")
		}
		_ = finish(failure)
		want := 0
		if !accepted {
			want = 1
		}
		if candidate.closes != want {
			t.Fatalf("candidate closes=%d want=%d", candidate.closes, want)
		}
	}
}
