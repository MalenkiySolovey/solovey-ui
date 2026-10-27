//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/option"

func InboundMembers(string, any) ([]option.Inbound, error) { return nil, nil }
