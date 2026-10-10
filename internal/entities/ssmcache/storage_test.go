package ssmcache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/service/ssmapi"
)

func testStore(t *testing.T, relative string) *Store {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	s, err := New(filepath.Join(Root(), relative))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOptionalCacheConstructionReadOnlyAndAtomicPrivateRoundtrip(t *testing.T) {
	s := testStore(t, "state.json")
	if _, err := os.Stat(Root()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructor created owner storage")
	}
	if _, err := s.Read(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing cache did not retain default")
	}
	data := []byte(`{"endpoints":{"/main":{"global_uplink":17}}}`)
	for i := 0; i < 2; i++ {
		if err := s.Write(context.Background(), data); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(context.Background())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("private state roundtrip failed")
	}
	for _, name := range []string{Root(), filepath.Join(Root(), "state.json")} {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		err = checkPrivate(f)
		f.Close()
		if err != nil {
			t.Fatal("created state not private")
		}
	}
	entries, _ := os.ReadDir(Root())
	if len(entries) != 1 {
		t.Fatal("temporary state leaked")
	}
}

func TestCacheRejectsUnownedPathsCorruptionAndCancelledWrite(t *testing.T) {
	s := testStore(t, "state.json")
	for _, name := range []string{"relative", Root(), filepath.Join(Root(), "..", "elsewhere"), filepath.Join(Root(), strings.Repeat("x", 1025))} {
		if _, err := New(name); err == nil {
			t.Fatal("unowned path accepted")
		}
	}
	preimage := []byte("{}")
	if err := s.Write(context.Background(), preimage); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Write(ctx, []byte(`{"endpoints":{}}`)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication accepted")
	}
	for _, data := range [][]byte{[]byte("null"), []byte(`{"endpoints":{"/main":null}}`), bytes.Repeat([]byte(" "), ssmapi.MaxCacheBytes+1)} {
		if err := s.Write(context.Background(), data); err == nil {
			t.Fatal("corrupt/excessive write accepted")
		}
	}
	got, err := s.Read(context.Background())
	if err != nil || !bytes.Equal(got, preimage) {
		t.Fatal("failed write changed preimage")
	}
	name := filepath.Join(Root(), "state.json")
	if err = os.WriteFile(name, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(context.Background()); err == nil {
		t.Fatal("corrupt read accepted")
	}
	raw, _ := json.Marshal(map[string]string{"cache_path": name})
	if findings := OptionsFindings("services", raw); len(findings) != 1 || findings[0].Code != "SSM_CACHE_CONTENT_REJECTED" {
		t.Fatal("corrupt state absent from redacted validation")
	}
	got, _ = os.ReadFile(name)
	if string(got) != "null" {
		t.Fatal("corrupt preimage changed")
	}
}

func TestImmutableRestoreSeedUsesSeparateWorkingState(t *testing.T) {
	s := testStore(t, filepath.Join("restored", "fixture", "generation", "state.seed"))
	seed := []byte(`{"endpoints":{"/main":{"global_uplink":17}}}`)
	if err := s.Write(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(s.root, s.relative)
	if NamespaceKey(name) != NamespaceKey(name+".state") {
		t.Fatal("seed and working state can acquire different service owners")
	}
	if err := os.Rename(name+".state", name); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read(context.Background()); err != nil || !bytes.Equal(got, seed) {
		t.Fatal("seed not loaded")
	}
	updated := []byte(`{"endpoints":{"/main":{"global_uplink":25}}}`)
	if err := s.Write(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read(context.Background()); err != nil || !bytes.Equal(got, updated) {
		t.Fatal("working state not loaded")
	}
	if got, _ := os.ReadFile(name); !bytes.Equal(got, seed) {
		t.Fatal("immutable restore preimage overwritten")
	}
}

func TestCacheConcurrentPublicationsAreCompleteAndClean(t *testing.T) {
	s := testStore(t, "state.json")
	if err := s.Write(context.Background(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			data, _ := json.Marshal(map[string]any{"endpoints": map[string]any{"/main": map[string]int{"global_uplink": i}}})
			if err := s.Write(context.Background(), data); err != nil {
				t.Error(err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if _, err := s.Read(context.Background()); err != nil {
		t.Fatal("torn concurrent state")
	}
	entries, _ := os.ReadDir(Root())
	if len(entries) != 1 {
		t.Fatal("concurrent temporary state leaked")
	}
}

func TestCacheValidationRedactedAndDuplicateOwnerRejected(t *testing.T) {
	s := testStore(t, "state.json")
	name := filepath.Join(s.root, s.relative)
	raw, _ := json.Marshal(map[string]string{"cache_path": name})
	if findings := OptionsFindings("services", raw); len(findings) != 0 {
		t.Fatal("owned missing state rejected")
	}
	source, _ := json.Marshal(map[string]any{"services": []any{map[string]string{"type": "ssm-api", "cache_path": name}, map[string]string{"type": "ssm-api", "cache_path": filepath.Join(s.root, "sub", "..", s.relative)}}})
	findings := ConfigFindings(source)
	if len(findings) != 1 || findings[0].Code != "SSM_CACHE_SHARED_PATH_REJECTED" {
		t.Fatal("shared cache owner accepted")
	}
	findings = OptionsFindings("services", []byte(`{"cache_path":"outside-private-marker"}`))
	encoded, _ := json.Marshal(findings)
	if len(findings) != 1 || strings.Contains(string(encoded), "outside-private-marker") {
		t.Fatal("unsafe path accepted or echoed")
	}
}
