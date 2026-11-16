package model

import entitytypes "github.com/MalenkiySolovey/solovey-ui/backend/entityidentity"

// VPNUserID projects the existing Client storage identity. Protocol credentials,
// subscription retrieval secrets and IP observations have separate lifecycles.
func (c Client) VPNUserID() entitytypes.VPNUserID { return entitytypes.VPNUserID(c.Id) }
