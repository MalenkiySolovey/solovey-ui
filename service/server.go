package service

import serversvc "github.com/MalenkiySolovey/solovey-ui/service/server"

// ServerService adapts server diagnostics to the application runtime.
type ServerService struct {
	serversvc.ServerService
}

// NewServerService adapts the application runtime to the narrow status callback
// needed by the server diagnostics package.
func NewServerService(runtime *Runtime) ServerService {
	runtime = runtimeOrDefault(runtime)
	backend := serversvc.New(func() (bool, uint32) {
		coreInstance := runtime.Core()
		if coreInstance == nil {
			return false, 0
		}
		uptime, running := coreInstance.Uptime()
		return running, uptime
	})
	return ServerService{ServerService: backend}
}
