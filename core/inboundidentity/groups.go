package inboundidentity

import (
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
)

func groupMemberTags(kind string, options any) ([]string, error) {
	members, err := registry.InboundMembers(kind, options)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(members))
	for _, child := range members {
		if child.Type == "bond" || child.Type == "failover" {
			return nil, errors.New("nested inbound group identity unavailable")
		}
		tags = append(tags, child.Tag)
	}
	return tags, nil
}
