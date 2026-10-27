package tagrefs

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/composition"
)

func inboundGroupReferences(rows []model.Inbound, tag string) ([]TagReference, error) {
	var refs []TagReference
	for _, row := range rows {
		members, err := composition.Members(row.Type, row.Options)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if member == tag {
				refs = append(refs, TagReference{Kind: "inbound group", Locator: fmt.Sprintf("inbound %q (inbounds)", row.Tag)})
			}
		}
	}
	return refs, nil
}
