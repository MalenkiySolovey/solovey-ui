//go:build !minimal

package remote

import "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"

type FormatResult struct {
	Format    string                   `json:"format"`
	Outbounds []map[string]interface{} `json:"-"`
	Error     string                   `json:"error,omitempty"`
}

type FetchAttempt struct {
	Variant string   `json:"variant"`
	Formats []string `json:"formats,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type FetchedSubscription struct {
	Outbounds []map[string]interface{}
	Snapshot  canonical.Snapshot
	Formats   []FormatResult
	Attempts  []FetchAttempt
}

// Complete reports whether every requested representation was fetched and
// parsed. A direct parse has no attempts and is complete by construction.
// Multi-variant fetches with even one failed variant are useful for upserts,
// but must not authorize deleting previously observed connections.
func (f *FetchedSubscription) Complete() bool {
	if f == nil {
		return false
	}
	if len(f.Attempts) == 0 {
		return true
	}
	for _, attempt := range f.Attempts {
		if attempt.Error != "" || len(attempt.Formats) == 0 {
			return false
		}
	}
	return true
}

type CollectionSnapshot struct {
	Formats  []FormatResult `json:"formats,omitempty"`
	Attempts []FetchAttempt `json:"attempts,omitempty"`
}
