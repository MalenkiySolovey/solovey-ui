package box

import F "github.com/sagernet/sing/common/format"

func indexedOptionTag(index int, tag string) string {
	if tag != "" {
		return tag
	}
	return F.ToString(index)
}
