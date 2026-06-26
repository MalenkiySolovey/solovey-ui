package client

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *Service) RotateSubSecret(id string) (string, error) {
	clientID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || clientID == 0 {
		return "", common.NewError("invalid client id")
	}
	db := clientDatabase()
	var client model.Client
	if err := db.Model(model.Client{}).Select("id, name").Where("id = ?", clientID).First(&client).Error; err != nil {
		return "", err
	}
	newSecret, err := common.RandomUUID()
	if err != nil {
		return "", err
	}
	if err := db.Model(model.Client{}).Where("id = ?", client.Id).Update("sub_secret", newSecret).Error; err != nil {
		return "", err
	}
	return client.Name, nil
}
