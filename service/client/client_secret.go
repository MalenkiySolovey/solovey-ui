package client

import (
	"strconv"

	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *Service) RotateSubSecret(id string) (string, error) {
	clientID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || clientID == 0 {
		return "", common.NewError("invalid client id")
	}
	return entityclients.RotateSubSecret(clientDatabase(), uint(clientID))
}
