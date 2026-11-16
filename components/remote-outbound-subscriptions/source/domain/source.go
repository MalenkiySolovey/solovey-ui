//go:build !minimal

package remote

import "errors"

const SourceRemote = "remote"
const SourceInline = "inline"
const SourceFile = "file"
const MaxInlineSourceBytes = 512 << 10

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
	return s.Id == other.Id && s.SourceKind() == other.SourceKind() && s.Url == other.Url && s.UserAgent == other.UserAgent && s.DownloadOutboundID == other.DownloadOutboundID && s.SourcePath == other.SourcePath && s.SourceContent == other.SourceContent && s.SourceHeaders == other.SourceHeaders && s.Include == other.Include && s.Exclude == other.Exclude && s.RemoveEmojis == other.RemoveEmojis && string(s.DialerOverride) == string(other.DialerOverride)
}
