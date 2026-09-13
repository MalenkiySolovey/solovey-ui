//go:build linux

package privilegedbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/procdexec"
)

const (
	procdQueryTimeout = 2 * time.Second
)

// ProcdInspector performs only the fixed ubus service-list operation required
// for broker peer attestation. It exposes no service lifecycle or arbitrary
// ubus command surface.
type ProcdInspector struct {
	run  func(context.Context, ClientManifest) ([]byte, error)
	ubus *procdexec.Object
}

func NewProcdInspector() *ProcdInspector {
	ubus, _ := procdexec.Open()
	return &ProcdInspector{ubus: ubus}
}

// Inspect performs only the manifest-bound service-list projection. Callers
// cannot supply ubus arguments or bypass ClientManifest validation.
func (p *ProcdInspector) Inspect(ctx context.Context, expected ClientManifest) (ProcdInstanceEvidence, error) {
	return p.inspect(ctx, expected)
}

func (p *ProcdInspector) inspect(ctx context.Context, expected ClientManifest) (procdInstanceEvidence, error) {
	if p == nil || (p.run == nil && p.ubus == nil) {
		return procdInstanceEvidence{}, errors.New("procd service evidence is unavailable")
	}
	if err := validateProcdManifestIdentity(expected); err != nil {
		return procdInstanceEvidence{}, err
	}
	raw, err := p.observe(ctx, expected)
	if err != nil {
		return procdInstanceEvidence{}, err
	}
	return parseProcdInstanceEvidence(raw, expected)
}

func (p *ProcdInspector) inspectAncestor(ctx context.Context, expected ClientManifest, peerPID int) (procdInstanceEvidence, error) {
	if p == nil || (p.run == nil && p.ubus == nil) || peerPID <= 1 || expected.ProcdRelation != ProcdRelationAncestor {
		return procdInstanceEvidence{}, errors.New("procd ancestor evidence is unavailable")
	}
	if err := validateProcdManifestIdentity(expected); err != nil {
		return procdInstanceEvidence{}, err
	}
	raw, err := p.observe(ctx, expected)
	if err != nil {
		return procdInstanceEvidence{}, err
	}
	return parseProcdAncestorEvidence(raw, expected, func(supervisorPID int) bool {
		return processDescendsFrom(peerPID, supervisorPID, 32)
	})
}

func (p *ProcdInspector) observe(ctx context.Context, expected ClientManifest) ([]byte, error) {
	if p.run != nil {
		return p.run(ctx, expected)
	}
	return runProcdServiceList(ctx, p.ubus, expected)
}

func runProcdServiceList(ctx context.Context, ubus *procdexec.Object, expected ClientManifest) ([]byte, error) {
	if ubus == nil || ubus.File() == nil || ubus.ExecPath(0) == "" {
		return nil, errors.New("procd executable authority is unavailable")
	}
	if err := ubus.Revalidate(); err != nil {
		return nil, errors.New("procd executable authority changed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	query, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: expected.ProcdService})
	if err != nil {
		return nil, errors.New("procd service query is invalid")
	}
	queryContext, cancel := context.WithTimeout(ctx, procdQueryTimeout)
	defer cancel()
	stdout, stderr := &procdBoundedBuffer{limit: maxProcdEvidenceBytes}, &procdBoundedBuffer{limit: maxProcdEvidenceBytes}
	command := exec.CommandContext(queryContext, ubus.ExecPath(0), "-S", "call", "service", "list", string(query))
	command.Args[0] = ubus.Label()
	command.ExtraFiles = []*os.File{ubus.File()}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil || stdout.overflow || stderr.overflow || len(stdout.Bytes()) == 0 {
		return nil, errors.New("bounded procd service observation failed")
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

type procdBoundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *procdBoundedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return written, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(value)
	return written, nil
}
