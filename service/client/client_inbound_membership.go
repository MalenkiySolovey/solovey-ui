package client

import (
	"encoding/json"

	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
)

func decodeClientInbounds(clientID uint, raw json.RawMessage, operation string) ([]uint, bool) {
	return entityclients.DecodeInbounds(clientID, raw, operation)
}
