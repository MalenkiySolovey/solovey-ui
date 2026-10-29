//go:build !minimal

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
	"gorm.io/gorm"
)

var ErrSourceHeadersInvalid = errors.New("SUBSCRIPTION_SOURCE_HEADERS_INVALID")

func parseSourceHeaders(raw []byte) (http.Header, error) {
	var fields map[string]json.RawMessage
	if len(raw) > 16<<10 || jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &fields) != nil {
		return nil, ErrSourceHeadersInvalid
	}
	headers := make(http.Header, len(fields))
	for name, value := range fields {
		key := http.CanonicalHeaderKey(name)
		if _, duplicate := headers[key]; duplicate {
			return nil, ErrSourceHeadersInvalid
		}
		var single string
		var multiple []string
		if json.Unmarshal(value, &single) == nil && string(value) != "null" {
			multiple = []string{single}
		} else {
			var entries []json.RawMessage
			if json.Unmarshal(value, &entries) != nil {
				return nil, ErrSourceHeadersInvalid
			}
			for _, entry := range entries {
				if string(entry) == "null" || json.Unmarshal(entry, &single) != nil {
					return nil, ErrSourceHeadersInvalid
				}
				multiple = append(multiple, single)
			}
		}
		headers[key] = multiple
	}
	if err := subexternal.ValidateFetchHeaders(headers); err != nil {
		return nil, ErrSourceHeadersInvalid
	}
	return headers, nil
}

func projectSourceHeaders(s *RemoteOutboundSubscription) error {
	if s.SourceKind() != SourceRemote {
		s.FetchHeaders = nil
		return nil
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "headers")
	if err != nil {
		return err
	}
	s.FetchHeaders, err = json.Marshal(ownerdb.View(ref, s.SourceHeaders))
	return err
}

// Headers may carry arbitrary provider credentials. Keep the complete map in
// the existing row-bound secret owner; never infer which names are sensitive.
func ApplySourceHeaders(tx *gorm.DB, s *RemoteOutboundSubscription, mutation json.RawMessage) error {
	if len(mutation) == 0 {
		return nil
	}
	if s.SourceKind() != SourceRemote {
		return ErrSourceHeadersInvalid
	}
	var intent struct{ Intent, Value string }
	if json.Unmarshal(mutation, &intent) != nil {
		return ErrSourceHeadersInvalid
	}
	if intent.Intent == "replace" {
		if _, err := parseSourceHeaders([]byte(intent.Value)); err != nil {
			return err
		}
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "headers")
	if err != nil {
		return err
	}
	s.SourceHeaders, err = ownerdb.Apply(tx, ref, s.SourceHeaders, mutation)
	return err
}

func fetchRemoteSource(ctx context.Context, db *gorm.DB, s RemoteOutboundSubscription, options FetchOptions) (*FetchedSubscription, error) {
	if s.SourceHeaders == "" {
		return FetchSubscriptionWithOptionsContext(ctx, s.Url, options)
	}
	ref, err := ownerdb.Reference(sourceOwner, s.Id, "headers")
	if err != nil {
		return nil, err
	}
	var fetched *FetchedSubscription
	_, err = ownerdb.Access(ctx, db, secretref.Access{Reference: ref, Actor: secretref.Actor{Component: sourceOwner}, Operation: secretref.UseForOwner, IntentID: "fetch-remote-source"}, s.SourceHeaders, func(raw []byte) error {
		headers, parseErr := parseSourceHeaders(raw)
		if parseErr != nil {
			return parseErr
		}
		options.Headers = headers
		fetched, parseErr = FetchSubscriptionWithOptionsContext(ctx, s.Url, options)
		if fetched != nil {
			for i := range fetched.Formats {
				if fetched.Formats[i].Error != "" {
					fetched.Formats[i].Error = "SUBSCRIPTION_PRIVATE_FETCH_FAILED"
				}
			}
			for i := range fetched.Attempts {
				if fetched.Attempts[i].Error != "" {
					fetched.Attempts[i].Error = "SUBSCRIPTION_PRIVATE_FETCH_FAILED"
				}
			}
		}
		return parseErr
	})
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("SUBSCRIPTION_PRIVATE_FETCH_FAILED")
	}
	return fetched, nil
}
