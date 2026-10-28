//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsSudokuOutbound = false

func registerSudokuOutbound(*outbound.Registry) {}
