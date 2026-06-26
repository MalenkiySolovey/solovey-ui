package apply

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type MutationRequest struct {
	Tx        *gorm.DB
	Object    string
	Action    string
	Data      json.RawMessage
	InitUsers string
	Hostname  string
}

type Handler func(MutationRequest, *Plan) error

type Router struct {
	handlers map[Object]Handler
}

func NewRouter(handlers map[Object]Handler) Router {
	copied := make(map[Object]Handler, len(handlers))
	for object, handler := range handlers {
		copied[object] = handler
	}
	return Router{handlers: copied}
}

func (r Router) Apply(req MutationRequest, plan *Plan) error {
	object, ok := ParseObject(req.Object)
	if !ok {
		return common.NewError("unknown object:", req.Object)
	}
	handler, ok := r.handlers[object]
	if !ok || handler == nil {
		return common.NewError("missing handler for object:", req.Object)
	}
	return handler(req, plan)
}

func (r Router) HandlerObjectStrings() []string {
	objects := make([]string, 0, len(r.handlers))
	for object := range r.handlers {
		objects = append(objects, object.String())
	}
	return objects
}
