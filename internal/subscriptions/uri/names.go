package uri

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
)

// UniqueLinkNames changes only duplicate nonempty public labels. It keeps wire
// credentials/endpoints unchanged and uses the same allocator as JSON/Clash.
func UniqueLinkNames(links []string) ([]string, error) {
	labels := make([]string, len(links))
	for i, link := range links {
		label, err := linkName(link)
		if err != nil {
			return nil, err
		}
		labels[i] = label
	}
	unique := canonical.UniqueLabels(labels)
	result := append([]string(nil), links...)
	for i, label := range labels {
		if label == "" || label == unique[i] {
			continue
		}
		var err error
		result[i], err = withLinkName(links[i], unique[i])
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func linkName(link string) (string, error) {
	if strings.HasPrefix(link, "vmess://") {
		var data map[string]any
		raw, err := codec.Decode(strings.TrimPrefix(link, "vmess://"))
		if err != nil || json.Unmarshal(raw, &data) != nil {
			return "", errors.New("invalid VMess public label")
		}
		label, _ := data["ps"].(string)
		return label, nil
	}
	u, err := url.Parse(link)
	if err != nil {
		return "", errors.New("invalid URI public label")
	}
	return u.Fragment, nil
}

func withLinkName(link, label string) (string, error) {
	if strings.HasPrefix(link, "vmess://") {
		var data map[string]any
		raw, err := codec.Decode(strings.TrimPrefix(link, "vmess://"))
		if err != nil || json.Unmarshal(raw, &data) != nil {
			return "", errors.New("invalid VMess public label")
		}
		data["ps"] = label
		raw, err = json.Marshal(data)
		if err != nil {
			return "", errors.New("invalid VMess public label")
		}
		return "vmess://" + codec.Encode(raw), nil
	}
	u, err := url.Parse(link)
	if err != nil {
		return "", errors.New("invalid URI public label")
	}
	u.Fragment, u.RawFragment = label, ""
	return u.String(), nil
}
