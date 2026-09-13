package sshbroker

import (
	"errors"
	"strings"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type Implementation string
type ServiceControl string
type LogEvidence string

const (
	ImplementationOpenSSH  Implementation = "openssh"
	ImplementationDropbear Implementation = "dropbear"

	ServiceControlSystemd ServiceControl = "systemd"
	ServiceControlProcd   ServiceControl = "procd"

	LogEvidenceJournald LogEvidence = "journald"
	LogEvidenceLogread  LogEvidence = "logread"
)

// Composition is deployment-owned input wiring. Validate checks only the
// three independent capability dimensions; it does not turn today's
// registered profiles into a semantic SSH invariant.
type Composition struct {
	Implementation Implementation
	ServiceControl ServiceControl
	LogEvidence    LogEvidence
}

// serviceControlTarget is the exact deployment binding consumed by a reusable
// supervisor adapter. Values exist only in the trusted release catalog; they
// are never accepted from broker requests or runtime arguments.
type serviceControlTarget struct {
	systemdUnits []string
	procdService string
}

type logEvidenceTarget struct {
	journaldUnits []string
}

type resolvedServiceControlTarget struct {
	systemdUnits [4]string
	unitCount    uint8
	procdService string
}

type resolvedLogEvidenceTarget struct {
	journaldUnits [4]string
	unitCount     uint8
}

// ResolvedSSHComposition is the result of resolving one SSH composition
// against this release's closed catalog. The semantic dimensions and target
// bindings are intentionally private: callers can only obtain them from the
// resolver and can only read defensive projections below.
type ResolvedSSHComposition struct {
	composition   Composition
	serviceTarget resolvedServiceControlTarget
	logTarget     resolvedLogEvidenceTarget
}

// registeredSSHComposition is the owner-local catalog record. It is not a
// second catalog or authority, and never crosses the package boundary.
type registeredSSHComposition struct {
	composition   Composition
	serviceTarget serviceControlTarget
	logTarget     logEvidenceTarget
}

func (resolved ResolvedSSHComposition) Valid() bool {
	serviceTarget := resolved.serviceTarget.catalogTarget()
	logTarget := resolved.logTarget.catalogTarget()
	if resolved.composition.Validate() != nil ||
		serviceTarget.validateFor(resolved.composition.ServiceControl) != nil ||
		logTarget.validateFor(resolved.composition.LogEvidence) != nil {
		return false
	}
	for _, candidate := range releaseSSHCompositionCatalog() {
		if candidate.composition != resolved.composition ||
			candidate.serviceTarget.validateFor(candidate.composition.ServiceControl) != nil ||
			candidate.logTarget.validateFor(candidate.composition.LogEvidence) != nil {
			continue
		}
		if sameStringSlices(candidate.serviceTarget.systemdUnits, serviceTarget.systemdUnits) &&
			candidate.serviceTarget.procdService == serviceTarget.procdService &&
			sameStringSlices(candidate.logTarget.journaldUnits, logTarget.journaldUnits) {
			return true
		}
	}
	return false
}

// Composition returns a copy of the untrusted input dimensions that were
// resolved. Mutating the returned value cannot mutate the trusted record.
func (resolved ResolvedSSHComposition) Composition() Composition {
	return resolved.composition
}

// Implementation returns the resolved SSH implementation dimension.
func (resolved ResolvedSSHComposition) Implementation() Implementation {
	return resolved.composition.Implementation
}

// ServiceControl returns the resolved service-control dimension.
func (resolved ResolvedSSHComposition) ServiceControl() ServiceControl {
	return resolved.composition.ServiceControl
}

// LogEvidence returns the resolved log-evidence dimension.
func (resolved ResolvedSSHComposition) LogEvidence() LogEvidence {
	return resolved.composition.LogEvidence
}

func (target serviceControlTarget) freeze() resolvedServiceControlTarget {
	result := resolvedServiceControlTarget{procdService: target.procdService}
	result.unitCount = uint8(len(target.systemdUnits))
	copy(result.systemdUnits[:], target.systemdUnits)
	return result
}

func (target resolvedServiceControlTarget) systemdUnitList() []string {
	if target.unitCount > uint8(len(target.systemdUnits)) {
		return nil
	}
	return append([]string(nil), target.systemdUnits[:target.unitCount]...)
}

func (target resolvedServiceControlTarget) catalogTarget() serviceControlTarget {
	return serviceControlTarget{systemdUnits: target.systemdUnitList(), procdService: target.procdService}
}

func (target logEvidenceTarget) freeze() resolvedLogEvidenceTarget {
	result := resolvedLogEvidenceTarget{}
	result.unitCount = uint8(len(target.journaldUnits))
	copy(result.journaldUnits[:], target.journaldUnits)
	return result
}

func (target resolvedLogEvidenceTarget) journaldUnitList() []string {
	if target.unitCount > uint8(len(target.journaldUnits)) {
		return nil
	}
	return append([]string(nil), target.journaldUnits[:target.unitCount]...)
}

func (target resolvedLogEvidenceTarget) catalogTarget() logEvidenceTarget {
	return logEvidenceTarget{journaldUnits: target.journaldUnitList()}
}

func (target logEvidenceTarget) validateFor(evidence LogEvidence) error {
	switch evidence {
	case LogEvidenceJournald:
		if len(target.journaldUnits) == 0 || len(target.journaldUnits) > 4 {
			return errors.New("SSH journald target binding is invalid")
		}
		seen := make(map[string]struct{}, len(target.journaldUnits))
		for _, unit := range target.journaldUnits {
			if !validServiceTargetToken(unit, 128) || !strings.HasSuffix(unit, ".service") {
				return errors.New("SSH journald target binding is invalid")
			}
			if _, duplicate := seen[unit]; duplicate {
				return errors.New("SSH journald target binding is ambiguous")
			}
			seen[unit] = struct{}{}
		}
	case LogEvidenceLogread:
		if len(target.journaldUnits) != 0 {
			return errors.New("SSH logread target binding is invalid")
		}
	default:
		return errors.New("SSH log-evidence target binding is unsupported")
	}
	return nil
}

func (c Composition) Validate() error {
	implementationValid := c.Implementation == ImplementationOpenSSH || c.Implementation == ImplementationDropbear
	controlValid := c.ServiceControl == ServiceControlSystemd || c.ServiceControl == ServiceControlProcd
	logValid := c.LogEvidence == LogEvidenceJournald || c.LogEvidence == LogEvidenceLogread
	if !implementationValid || !controlValid || !logValid {
		return errors.New("SSH composition contains an unknown adapter")
	}
	return nil
}

func (target serviceControlTarget) validateFor(control ServiceControl) error {
	switch control {
	case ServiceControlSystemd:
		if target.procdService != "" || len(target.systemdUnits) == 0 || len(target.systemdUnits) > 4 {
			return errors.New("SSH systemd target binding is invalid")
		}
		seen := make(map[string]struct{}, len(target.systemdUnits))
		for _, unit := range target.systemdUnits {
			if !validServiceTargetToken(unit, 128) || !strings.HasSuffix(unit, ".service") {
				return errors.New("SSH systemd target binding is invalid")
			}
			if _, duplicate := seen[unit]; duplicate {
				return errors.New("SSH systemd target binding is ambiguous")
			}
			seen[unit] = struct{}{}
		}
	case ServiceControlProcd:
		if len(target.systemdUnits) != 0 || !validServiceTargetToken(target.procdService, 64) {
			return errors.New("SSH procd target binding is invalid")
		}
	default:
		return errors.New("SSH service-control target binding is unsupported")
	}
	return nil
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validServiceTargetToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._@-", char) {
			continue
		}
		return false
	}
	return true
}

func releaseSSHCompositionCatalog() []registeredSSHComposition {
	return []registeredSSHComposition{
		{composition: SystemdOpenSSHComposition(), serviceTarget: serviceControlTarget{systemdUnits: []string{"ssh.service", "sshd.service"}},
			logTarget: logEvidenceTarget{journaldUnits: []string{"ssh.service", "sshd.service"}}},
		{composition: ProcdDropbearComposition(), serviceTarget: serviceControlTarget{procdService: "dropbear"}},
	}
}

func resolveRegisteredSSHComposition(composition Composition, catalog []registeredSSHComposition) (registeredSSHComposition, error) {
	if err := composition.Validate(); err != nil {
		return registeredSSHComposition{}, err
	}
	var selected registeredSSHComposition
	matches := 0
	for _, candidate := range catalog {
		if candidate.composition != composition {
			continue
		}
		if err := candidate.serviceTarget.validateFor(candidate.composition.ServiceControl); err != nil {
			return registeredSSHComposition{}, err
		}
		if err := candidate.logTarget.validateFor(candidate.composition.LogEvidence); err != nil {
			return registeredSSHComposition{}, err
		}
		selected = candidate
		selected.serviceTarget.systemdUnits = append([]string(nil), candidate.serviceTarget.systemdUnits...)
		selected.logTarget.journaldUnits = append([]string(nil), candidate.logTarget.journaldUnits...)
		matches++
	}
	if matches != 1 {
		return registeredSSHComposition{}, errors.New("SSH composition adapters are not registered unambiguously by this release")
	}
	return selected, nil
}

// ResolveRegisteredSSHComposition is the single release composition resolver.
// Every privileged consumer must receive the returned record rather than
// reinterpreting selectors or target strings independently.
func ResolveRegisteredSSHComposition(composition Composition) (ResolvedSSHComposition, error) {
	registered, err := resolveRegisteredSSHComposition(composition, releaseSSHCompositionCatalog())
	if err != nil {
		return ResolvedSSHComposition{}, err
	}
	resolved := ResolvedSSHComposition{composition: registered.composition,
		serviceTarget: registered.serviceTarget.freeze(),
		logTarget:     registered.logTarget.freeze()}
	if !resolved.Valid() {
		return ResolvedSSHComposition{}, errors.New("resolved SSH composition is not valid")
	}
	return resolved, nil
}

// registered reports whether all concrete adapters needed by this deployment
// profile are shipped in the current release. This is a construction-catalog
// decision, deliberately separate from dimension and policy validation.
func (c Composition) registered() bool {
	_, err := ResolveRegisteredSSHComposition(c)
	return err == nil
}

func (c Composition) validateRegistered() error {
	_, err := ResolveRegisteredSSHComposition(c)
	return err
}

func SystemdOpenSSHComposition() Composition {
	return Composition{Implementation: ImplementationOpenSSH, ServiceControl: ServiceControlSystemd, LogEvidence: LogEvidenceJournald}
}

func ProcdDropbearComposition() Composition {
	return Composition{Implementation: ImplementationDropbear, ServiceControl: ServiceControlProcd, LogEvidence: LogEvidenceLogread}
}

// ParseRuntimeArgs accepts one explicit value for every independent runtime
// dimension. It deliberately has no AUTO or transport-derived defaults.
func ParseRuntimeArgs(args []string) (broker.TransportMode, ResolvedSSHComposition, error) {
	if len(args) != 4 {
		return "", ResolvedSSHComposition{}, errors.New("privileged broker requires explicit transport and SSH composition")
	}
	values := make(map[string]string, len(args))
	for _, argument := range args {
		parts := strings.SplitN(argument, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", ResolvedSSHComposition{}, errors.New("privileged broker composition argument is malformed")
		}
		if _, exists := values[parts[0]]; exists {
			return "", ResolvedSSHComposition{}, errors.New("privileged broker composition argument is duplicated")
		}
		values[parts[0]] = parts[1]
	}
	transport, err := broker.ParseTransportArgs([]string{"--transport=" + values["--transport"]})
	if err != nil {
		return "", ResolvedSSHComposition{}, err
	}
	composition := Composition{Implementation: Implementation(values["--ssh-implementation"]),
		ServiceControl: ServiceControl(values["--ssh-service-control"]), LogEvidence: LogEvidence(values["--ssh-log-evidence"])}
	resolved, err := ResolveRegisteredSSHComposition(composition)
	if err != nil {
		return "", ResolvedSSHComposition{}, err
	}
	if len(values) != 4 {
		return "", ResolvedSSHComposition{}, errors.New("privileged broker composition argument is unsupported")
	}
	return transport, resolved, nil
}
