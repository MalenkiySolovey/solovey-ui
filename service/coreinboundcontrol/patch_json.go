package coreinboundcontrol

import (
	"encoding/json"
	"fmt"
	"sort"
)

type checkpointTargetV1 struct {
	Server string `json:"server"`
	Port   uint16 `json:"server_port"`
}

type checkpointALPNTargetV1 struct {
	ALPN   string             `json:"alpn"`
	Target checkpointTargetV1 `json:"target"`
}

func decodeObject(content []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(content, &object); err != nil || object == nil {
		return nil, fmt.Errorf("invalid object")
	}
	return object, nil
}

func decodeTarget(content json.RawMessage) (checkpointTargetV1, error) {
	object, err := decodeObject(content)
	if err != nil {
		return checkpointTargetV1{}, err
	}
	var target checkpointTargetV1
	if err = json.Unmarshal(object["server"], &target.Server); err != nil || target.Server == "" {
		return checkpointTargetV1{}, fmt.Errorf("invalid target server")
	}
	if err = json.Unmarshal(object["server_port"], &target.Port); err != nil || target.Port == 0 {
		return checkpointTargetV1{}, fmt.Errorf("invalid target port")
	}
	return target, nil
}

func encodeTarget(target checkpointTargetV1) json.RawMessage {
	content, _ := json.Marshal(struct {
		Server string `json:"server"`
		Port   uint16 `json:"server_port"`
	}{target.Server, target.Port})
	return content
}

func patchRealityHandshake(content []byte, target checkpointTargetV1) ([]byte, checkpointTargetV1, error) {
	tlsObject, err := decodeObject(content)
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	reality, err := decodeObject(tlsObject["reality"])
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	handshake, err := decodeObject(reality["handshake"])
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	previous, err := decodeTarget(reality["handshake"])
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	handshake["server"], _ = json.Marshal(target.Server)
	handshake["server_port"], _ = json.Marshal(target.Port)
	reality["handshake"], _ = json.Marshal(handshake)
	tlsObject["reality"], _ = json.Marshal(reality)
	patched, err := json.Marshal(tlsObject)
	return patched, previous, err
}

func patchTrojanFallback(content []byte, target checkpointTargetV1) ([]byte, checkpointTargetV1, error) {
	options, err := decodeObject(content)
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	previous, err := decodeTarget(options["fallback"])
	if err != nil {
		return nil, checkpointTargetV1{}, err
	}
	options["fallback"] = encodeTarget(target)
	patched, err := json.Marshal(options)
	return patched, previous, err
}

func patchTrojanALPN(content []byte, alpn []string, target checkpointTargetV1, replaceDefault bool) ([]byte, []checkpointALPNTargetV1, *checkpointTargetV1, error) {
	options, err := decodeObject(content)
	if err != nil {
		return nil, nil, nil, err
	}
	previousMap, err := decodeObject(options["fallback_for_alpn"])
	if err != nil {
		return nil, nil, nil, err
	}
	keys := make([]string, 0, len(previousMap))
	for key := range previousMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	previous := make([]checkpointALPNTargetV1, 0, len(keys))
	for _, key := range keys {
		value, targetErr := decodeTarget(previousMap[key])
		if targetErr != nil {
			return nil, nil, nil, targetErr
		}
		previous = append(previous, checkpointALPNTargetV1{ALPN: key, Target: value})
	}
	if !equalExactStrings(keys, sortedCopy(alpn)) {
		return nil, nil, nil, fmt.Errorf("ALPN map is not exhaustive")
	}
	replacement := make(map[string]json.RawMessage, len(alpn))
	for _, value := range alpn {
		replacement[value] = encodeTarget(target)
	}
	options["fallback_for_alpn"], _ = json.Marshal(replacement)
	var previousDefault *checkpointTargetV1
	if replaceDefault {
		value, targetErr := decodeTarget(options["fallback"])
		if targetErr != nil {
			return nil, nil, nil, targetErr
		}
		previousDefault = &value
		options["fallback"] = encodeTarget(target)
	}
	patched, err := json.Marshal(options)
	return patched, previous, previousDefault, err
}

func restoreTrojanALPN(content []byte, previous []checkpointALPNTargetV1, previousDefault *checkpointTargetV1) ([]byte, error) {
	options, err := decodeObject(content)
	if err != nil {
		return nil, err
	}
	restored := make(map[string]json.RawMessage, len(previous))
	for _, item := range previous {
		restored[item.ALPN] = encodeTarget(item.Target)
	}
	options["fallback_for_alpn"], _ = json.Marshal(restored)
	if previousDefault != nil {
		options["fallback"] = encodeTarget(*previousDefault)
	}
	return json.Marshal(options)
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func equalExactStrings(left, right []string) bool {
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
