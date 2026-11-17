package users

import (
	"bytes"
	"encoding/json"

	clientfacts "github.com/MalenkiySolovey/solovey-host/subscriptions/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/backend/users/credentials"

	"gorm.io/gorm"
)

// DeliveryCopy borrows assigned, currently deliverable credentials after the
// subscription owner authorizes its client. It never changes canonical state.
// The caller wipes the returned Config after rendering the private response.
func (s CredentialManager) DeliveryCopy(db *gorm.DB, client *model.Client, inbounds []*model.Inbound) (*model.Client, error) {
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
			clear(fields[field])
		}
	}()
	for _, inbound := range inbounds {
		if inbound == nil {
			continue
		}
		fact, known := clientfacts.Resolve(inbound.Type)
		if !known || fact.SecretField == "" || borrowed[fact.UserField] ||
			(!s.canDeliver(fact.Type, "json") && !s.canDeliver(fact.Type, "uri")) {
			continue
		}
		if err := credentials.UseClient(db.Statement.Context, db, client.Id, fact.Type, fields[fact.UserField], "subscription-delivery", func(value []byte) error {
			fields[fact.UserField] = bytes.Clone(value)
			borrowed[fact.UserField] = true
			return nil
		}, s.Warning); err != nil {
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
