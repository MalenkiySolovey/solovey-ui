//go:build !minimal

package remote

import (
	"errors"
	"regexp"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

var ErrSourceFilterInvalid = errors.New("SUBSCRIPTION_SOURCE_FILTER_INVALID")
var ErrSourceFilterDependency = errors.New("SUBSCRIPTION_SOURCE_FILTER_DEPENDENCY: a retained group references an excluded connection")

func (s RemoteOutboundSubscription) sourceFilters() (include, exclude *regexp.Regexp, err error) {
	patterns := []string{s.Include, s.Exclude}
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		if len(pattern) > 4096 {
			return nil, nil, ErrSourceFilterInvalid
		}
		if pattern != "" {
			compiled[i], err = regexp.Compile(pattern)
			if err != nil {
				return nil, nil, ErrSourceFilterInvalid
			}
		}
	}
	return compiled[0], compiled[1], nil
}

func (s RemoteOutboundSubscription) ValidateSourceFilters() error {
	_, _, err := s.sourceFilters()
	return err
}

// Filters select existing IR observations by their source label. They do not
// rewrite a protocol, credential, transport or the captured source formats.
func ApplySourceFilters(s RemoteOutboundSubscription, fetched *FetchedSubscription) (*FetchedSubscription, error) {
	include, exclude, err := s.sourceFilters()
	if err != nil {
		return nil, err
	}
	if fetched == nil || include == nil && exclude == nil {
		return fetched, nil
	}
	selected := make(map[string]bool, len(fetched.Snapshot.Connections))
	connections := make([]canonical.Connection, 0, len(fetched.Snapshot.Connections))
	for _, connection := range fetched.Snapshot.Connections {
		name := connection.DisplayName
		keep := (include == nil || include.MatchString(name)) && (exclude == nil || !exclude.MatchString(name))
		selected[name] = keep
		if keep {
			connections = append(connections, connection)
		}
	}
	checkMembers := func(members []string) error {
		for _, member := range members {
			if keep, known := selected[member]; known && !keep {
				return ErrSourceFilterDependency
			}
		}
		return nil
	}
	for _, connection := range connections {
		if err := checkMembers(connection.GroupMembers); err != nil {
			return nil, err
		}
	}
	// Captured profile groups remain owned observations. Do not publish a
	// silently truncated group or leave it referring to a filtered endpoint.
	for _, extra := range fetched.Snapshot.Extras {
		if err := checkMembers(stringList(extra.Outbound["outbounds"])); err != nil {
			return nil, err
		}
	}
	result := *fetched
	result.Snapshot.Connections = connections
	result.Outbounds = canonical.SnapshotOutbounds(result.Snapshot)
	return &result, nil
}
