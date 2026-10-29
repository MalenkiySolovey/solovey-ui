//go:build !minimal

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"gorm.io/gorm"
)

const SourceRemote = "remote"
const SourceInline = "inline"
const MaxInlineSourceBytes = 512 << 10
const sourceOwner = "remote-outbound-subscriptions"

var ErrSourceContentInvalid = errors.New("SUBSCRIPTION_SOURCE_CONTENT_INVALID")

func (s RemoteOutboundSubscription) SourceKind() string {
	if s.SourceType == "" {
		return SourceRemote
	}
	return s.SourceType
}

// Source identity is captured before I/O and checked again in the committing
// transaction. A refresh of old content must not replace a newer source.
func (s RemoteOutboundSubscription) SameSource(other RemoteOutboundSubscription) bool {
	return s.Id == other.Id && s.SourceKind() == other.SourceKind() && s.Url == other.Url && s.SourceContent == other.SourceContent && s.Include == other.Include && s.Exclude == other.Exclude
}

func ProjectSourceContent(s *RemoteOutboundSubscription) error {
	s.SourceType = s.SourceKind()
	if s.SourceType != SourceInline {
		s.Content = nil
		return nil
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "content")
	if err != nil {
		return err
	}
	s.Content, err = json.Marshal(ownerdb.View(ref, s.SourceContent))
	return err
}

// The complete source may contain credentials anywhere in a supported client
// representation. Store it as one owner-bound value, without guessing paths.
func ApplySourceContent(ctx context.Context, tx *gorm.DB, s *RemoteOutboundSubscription, mutation json.RawMessage, options FetchOptions) error {
	if s.SourceKind() != SourceInline {
		if len(mutation) != 0 {
			return ErrSourceContentInvalid
		}
		return nil
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "content")
	if err != nil {
		return err
	}
	if len(mutation) != 0 {
		var intent struct {
			Intent string `json:"intent"`
			Value  string `json:"value"`
		}
		if json.Unmarshal(mutation, &intent) != nil || intent.Intent == "replace" && (len(intent.Value) == 0 || len(intent.Value) > MaxInlineSourceBytes || strings.ContainsRune(intent.Value, 0)) {
			return ErrSourceContentInvalid
		}
		s.SourceContent, err = ownerdb.Apply(tx, ref, s.SourceContent, mutation)
		if err != nil {
			return err
		}
	}
	_, err = FetchSourceContext(ctx, tx, *s, options)
	return err
}

func FetchSourceContext(ctx context.Context, db *gorm.DB, s RemoteOutboundSubscription, options FetchOptions) (*FetchedSubscription, error) {
	if s.SourceKind() == SourceRemote {
		fetched, err := FetchSubscriptionWithOptionsContext(ctx, s.Url, options)
		if err != nil {
			return nil, err
		}
		return ApplySourceFilters(s, fetched)
	}
	if s.SourceKind() != SourceInline || s.SourceContent == "" {
		return nil, ErrSourceContentInvalid
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "content")
	if err != nil {
		return nil, err
	}
	var fetched *FetchedSubscription
	_, err = ownerdb.Access(ctx, db, secretref.Access{Reference: ref, Actor: secretref.Actor{Component: sourceOwner}, Operation: secretref.UseForOwner, IntentID: "parse-inline-source"}, s.SourceContent, func(raw []byte) error {
		if len(raw) == 0 || len(raw) > MaxInlineSourceBytes {
			return ErrSourceContentInvalid
		}
		var parseErr error
		fetched, parseErr = ParseFetchedSubscriptionWithOptions(string(raw), options)
		if parseErr != nil {
			return ErrSourceContentInvalid
		}
		return nil
	})
	if err != nil {
		return nil, ErrSourceContentInvalid
	}
	return ApplySourceFilters(s, fetched)
}
