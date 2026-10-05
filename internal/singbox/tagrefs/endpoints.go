package tagrefs

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

// Endpoint uses the shared outbound namespace but has an owner-local lifetime
// distinction: pinned sing-box's default outbound retains an endpoint pointer,
// and EndpointManager replacement does not rebind that pointer.
func Endpoint(tx *gorm.DB, tag string, excludeID uint) ([]TagReference, error) {
	refs, err := Outbound(tx, tag, 0, excludeID)
	if err != nil {
		return nil, err
	}
	var services []model.Service
	if err := tx.Model(&model.Service{}).Select("id", "type", "tag", "options").Find(&services).Error; err != nil {
		return nil, err
	}
	refs = append(refs, endpointServiceReferences(services, tag)...)
	base, err := configBlobFrom(tx)
	if err != nil {
		return nil, err
	}
	additional, err := projectedBaseReferences(base, "endpoints", tag)
	if err != nil {
		return nil, err
	}
	return forEndpoint(append(refs, additional...)), nil
}

func forEndpoint(refs []TagReference) []TagReference {
	for index := range refs {
		if refs[index].Kind == "route final" {
			refs[index].Lazy = false
		}
	}
	return refs
}

type ReloadRejection struct {
	Tag        string
	References []TagReference
}

func (e *ReloadRejection) ReasonCode() string { return "FULL_RESTART_REQUIRED" }
func (e *ReloadRejection) Error() string {
	return fmt.Sprintf("%s: endpoint %q is captured at construction", e.ReasonCode(), e.Tag)
}
