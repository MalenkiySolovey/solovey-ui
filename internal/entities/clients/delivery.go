package entityclients

import (
	"bytes"
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// DeliveryCopy borrows assigned, currently deliverable credentials after the
// subscription owner authorizes its client. It never changes canonical state.
// The caller wipes the returned Config after rendering the private response.
func DeliveryCopy(db *gorm.DB, client *model.Client, inbounds []*model.Inbound) (*model.Client, error) {
	if db == nil || client == nil {
		return nil, credentials.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(client.Config, &fields) != nil || fields == nil {
		return nil, credentials.ErrInvalid
	}
	borrowed := map[string]bool{}
	defer func() {
		for field := range borrowed {
			common.WipeBytes(fields[field])
		}
	}()
	for _, inbound := range inbounds {
		if inbound == nil {
			continue
		}
		fact, known := clientfacts.Resolve(inbound.Type)
		if !known || fact.SecretField == "" || borrowed[fact.UserField] ||
			(!clientfacts.CanDeliver(fact.Type, "json") && !clientfacts.CanDeliver(fact.Type, "uri")) {
			continue
		}
		if err := credentials.UseClient(db.Statement.Context, db, client.Id, fact.Type, fields[fact.UserField], "subscription-delivery", func(value []byte) error {
			fields[fact.UserField] = bytes.Clone(value)
			borrowed[fact.UserField] = true
			return nil
		}); err != nil {
			return nil, err
		}
	}
	copy := *client
	var err error
	copy.Config, err = json.Marshal(fields)
	if err != nil {
		return nil, credentials.ErrInvalid
	}
	return &copy, nil
}
