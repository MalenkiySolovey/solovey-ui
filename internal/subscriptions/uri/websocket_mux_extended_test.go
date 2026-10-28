//go:build shtorm_extended

package uri

import mux "github.com/sagernet/sing-mux"

func configureWebsocketMuxConnectionContext(options *mux.ServiceOptions) {
	// The selected Shtorm mux requires a parent context hook in addition to
	// the stream hook. Both preserve this fixture's admitted context.
	options.NewConnectionContext = options.NewStreamContext
}
