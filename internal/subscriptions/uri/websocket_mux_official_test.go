//go:build !shtorm_extended

package uri

import mux "github.com/sagernet/sing-mux"

func configureWebsocketMuxConnectionContext(_ *mux.ServiceOptions) {}
