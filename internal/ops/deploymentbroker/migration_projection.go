package deploymentbroker

import "errors"

type transitionPart uint8

const (
	transitionForeign transitionPart = iota
	transitionOriginal
	transitionTarget
)

func transitionAuthoritySafe(unit, marker transitionPart) bool {
	return (unit == transitionOriginal || unit == transitionTarget) &&
		(marker == transitionOriginal || marker == transitionTarget)
}

type stagedDataFact struct {
	Name                 string
	UID                  uint32
	GID                  uint32
	Mode                 uint32
	Regular              bool
	MarkerContentMatches bool
}

func validateStagedDataFacts(facts []stagedDataFact, markerName string, serviceUID, serviceGID uint32, markerRequired bool) error {
	if len(facts) > 5 {
		return errors.New("staged database authority is ambiguous")
	}
	allowed := map[string]bool{"solovey-ui.db": true, "solovey-ui.db-wal": true, "solovey-ui.db-shm": true, "solovey-ui.db-journal": true, markerName: true}
	markerPresent := false
	for _, fact := range facts {
		if !fact.Regular || !allowed[fact.Name] {
			return errors.New("foreign newer data prevents deployment rollback")
		}
		if fact.Name == markerName {
			if fact.UID != 0 || fact.GID != 0 || fact.Mode != 0o400 || !fact.MarkerContentMatches {
				return errors.New("deployment data marker differs")
			}
			markerPresent = true
			continue
		}
		if fact.UID != serviceUID || fact.GID != serviceGID || fact.Mode&0o077 != 0 || fact.Mode&0o111 != 0 {
			return errors.New("staged database owner or mode differs")
		}
	}
	if markerRequired && !markerPresent {
		return errors.New("deployment data marker is missing after profile switch")
	}
	return nil
}
