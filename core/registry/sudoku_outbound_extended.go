//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/sudoku"
)

const supportsSudokuOutbound = true

func registerSudokuOutbound(registry *outbound.Registry) { sudoku.RegisterOutbound(registry) }
