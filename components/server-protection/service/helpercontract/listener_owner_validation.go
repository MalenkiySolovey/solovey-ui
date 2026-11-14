package helpercontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	hostfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	"sort"
	"strings"
)

func SealListenerOwnerResult(result *ListenerOwnerObserveResult) {
	if result == nil {
		return
	}
	for index := range result.Facts {
		result.Facts[index].Seal()
	}
	sort.Slice(result.Facts, func(i, j int) bool {
		return result.Facts[i].ObservationRevision < result.Facts[j].ObservationRevision
	})
	result.ReasonCodes = NormalizedOwnerReasons(result.ReasonCodes)
	copy := *result
	copy.ObservationRevision = ""
	copy.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), result.Facts...)
	for index := range copy.Facts {
		copy.Facts[index].ObservedAt, copy.Facts[index].ExpiresAt = 0, 0
	}
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	result.ObservationRevision = hex.EncodeToString(sum[:])
}

func ListenerOwnerResultValid(result *ListenerOwnerObserveResult) bool {
	if result == nil || len(strings.TrimSpace(result.ObservationRevision)) != 64 {
		return false
	}
	copy := *result
	copy.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), result.Facts...)
	copy.ReasonCodes = append([]string(nil), result.ReasonCodes...)
	SealListenerOwnerResult(&copy)
	return copy.ObservationRevision == result.ObservationRevision
}

func NormalizedOwnerReasons(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !correlationPattern.MatchString(value) {
			value = "listener_owner_reason_invalid"
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
