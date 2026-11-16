//go:build !minimal

package remote

type SourceHealthCheck struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url,omitempty"`
	Interval string `json:"interval,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}
