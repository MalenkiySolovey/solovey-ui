// Package manifest preserves the Panel integration import path for the public
// installed-component descriptor. Concrete lifecycle and storage stay Panel-owned.
package manifest

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"

type Delivery = canonical.Delivery

const DeliveryInProcess = canonical.DeliveryInProcess

type Manifest = canonical.Manifest
type Frontend = canonical.Frontend
type Packages = canonical.Packages

func ValidateID(id string) error { return canonical.ValidateID(id) }
