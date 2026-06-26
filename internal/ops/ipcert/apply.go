package ipcert

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func PatchTLSServerBlock(serverJSON json.RawMessage, certPath, keyPath string) (json.RawMessage, error) {
	server := map[string]json.RawMessage{}
	if len(serverJSON) > 0 {
		if err := json.Unmarshal(serverJSON, &server); err != nil {
			return nil, common.NewError("ip cert: tls server block is not an object: ", err.Error())
		}
	}
	if err := setJSONStringField(server, "certificate_path", certPath); err != nil {
		return nil, err
	}
	if err := setJSONStringField(server, "key_path", keyPath); err != nil {
		return nil, err
	}
	delete(server, "certificate")
	delete(server, "key")
	return json.Marshal(server)
}

func setJSONStringField(obj map[string]json.RawMessage, key, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	obj[key] = encoded
	return nil
}
