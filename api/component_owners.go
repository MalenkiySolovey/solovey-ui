package api

import "github.com/MalenkiySolovey/solovey-backend/componentroutes"

func (a *ApiService) componentOwners() ([]componentroutes.OwnerAvailability, error) {
	if a.Components == nil {
		return nil, componentroutes.ErrUnavailable
	}
	return a.Components.Owners()
}
