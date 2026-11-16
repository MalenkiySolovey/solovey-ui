package service

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
)

// SetComponentEnabled retains the canonical settings save transaction and its
// effects for both the requested state and lifecycle compensation.
func (s *ConfigService) SetComponentEnabled(id string, enabled bool, actor, hostname string) error {
	if s == nil {
		return errors.New("component settings authority is unavailable")
	}
	payload, err := json.Marshal(map[string]string{enabledstate.SettingKey(id): strconv.FormatBool(enabled)})
	if err != nil {
		return err
	}
	_, err = s.Save("settings", "set", payload, "", actor, hostname)
	return err
}
