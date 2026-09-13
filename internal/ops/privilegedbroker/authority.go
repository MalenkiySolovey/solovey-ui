package privilegedbroker

import (
	"context"
	"errors"
	"sync"
)

type authorityLease struct {
	server     *Server
	id         uint64
	Attestor   Attestor
	Revision   string
	Revisioned bool
	Context    context.Context
	once       sync.Once
}

func (l *authorityLease) Release() {
	if l == nil || l.server == nil {
		return
	}
	l.once.Do(func() { l.server.releaseAuthority(l.id) })
}

func attestorRevision(attestor Attestor) (string, bool) {
	revisioned, ok := attestor.(interface{ AuthorityRevision() string })
	if !ok || !digestPattern.MatchString(revisioned.AuthorityRevision()) {
		return "", false
	}
	return revisioned.AuthorityRevision(), true
}

func (s *Server) beginAuthority(parent context.Context) (*authorityLease, error) {
	if parent == nil {
		parent = context.Background()
	}
	s.authorityMu.Lock()
	defer s.authorityMu.Unlock()
	if s.retiring {
		return nil, attestationFailure(PeerAttestationGenerationMismatch, errors.New("broker authority generation is retiring"))
	}
	if s.authorityActive == 0 {
		s.authorityIdle = make(chan struct{})
	}
	s.authorityActive++
	s.authoritySequence++
	id := s.authoritySequence
	ctx, cancel := context.WithCancel(parent)
	s.authorityCancels[id] = cancel
	revision, revisioned := attestorRevision(s.Attestor)
	if revisioned && revision != s.authorityRevision {
		cancel()
		delete(s.authorityCancels, id)
		s.authorityActive--
		if s.authorityActive == 0 {
			close(s.authorityIdle)
		}
		return nil, attestationFailure(PeerAttestationGenerationMismatch, errors.New("broker authority generation is inconsistent"))
	}
	return &authorityLease{server: s, id: id, Attestor: s.Attestor, Revision: s.authorityRevision,
		Revisioned: revisioned, Context: ctx}, nil
}

func (s *Server) releaseAuthority(id uint64) {
	s.authorityMu.Lock()
	if cancel, ok := s.authorityCancels[id]; ok {
		cancel()
		delete(s.authorityCancels, id)
		if s.authorityActive > 0 {
			s.authorityActive--
		}
		if s.authorityActive == 0 {
			close(s.authorityIdle)
		}
	}
	s.authorityMu.Unlock()
}

func (s *Server) authorityCurrent(revision string) bool {
	s.authorityMu.Lock()
	defer s.authorityMu.Unlock()
	return !s.retiring && s.authorityRevision == revision
}

// RotateAuthority installs one fully validated attestor generation. Rotation
// first fences new requests, cancels and closes old-generation connections,
// waits for their bounded handlers to drain, and only then exposes the new
// sealed generation.
func (s *Server) RotateAuthority(ctx context.Context, next Attestor, revision string) error {
	if s == nil || next == nil || !digestPattern.MatchString(revision) {
		return errors.New("broker authority generation is invalid")
	}
	if actual, revisioned := attestorRevision(next); revisioned && actual != revision {
		return errors.New("broker attestor generation differs from requested rotation")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.authorityMu.Lock()
	if s.retiring {
		s.authorityMu.Unlock()
		return errors.New("broker authority generation is already retiring")
	}
	if s.authorityRevision == revision {
		s.authorityMu.Unlock()
		return nil
	}
	s.retiring = true
	for _, cancel := range s.authorityCancels {
		cancel()
	}
	idle := s.authorityIdle
	s.authorityMu.Unlock()
	s.ShutdownConnections()
	select {
	case <-idle:
	case <-ctx.Done():
		// The retiring fence deliberately remains closed. A subsequent operator
		// reconciliation can retry; no request is admitted under uncertain state.
		return errors.New("broker authority generation did not drain within its bound")
	}
	s.authorityMu.Lock()
	s.Attestor = next
	s.authorityRevision = revision
	s.retiring = false
	s.authorityMu.Unlock()
	return nil
}

func (s *Server) AuthorityRevision() string {
	if s == nil {
		return ""
	}
	s.authorityMu.Lock()
	defer s.authorityMu.Unlock()
	return s.authorityRevision
}

func (a ManifestAttestor) AuthorityRevision() string { return a.Manifest.Revision }
