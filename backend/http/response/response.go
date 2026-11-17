package response

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-host/redact"
	"github.com/gin-gonic/gin"
)

type Writer struct{ Warning func(...any) }

type Msg struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

const MaxClientErrorBytes = 4 * 1024

func (w Writer) JSONMsg(c *gin.Context, msg string, err error) {
	w.JSONMsgObj(c, msg, nil, err)
}

func (w Writer) JSONObj(c *gin.Context, obj interface{}, err error) {
	w.JSONMsgObj(c, "", obj, err)
}

// jsonMsgObj writes the standard API envelope (Msg) at HTTP 200 with a success
// flag. This SPA contract is intentional: business/validation errors are
// conveyed as {success:false, msg} at HTTP 200 (the frontend reads the flag),
// not via HTTP status codes; transport-level handlers that genuinely need real
// status codes set them explicitly. The
// client-facing error text is redacted to avoid leaking paths/SQL/internals;
// the corresponding server log is independently redacted and bounded.
func (w Writer) JSONMsgObj(c *gin.Context, msg string, obj interface{}, err error) {
	m := Msg{
		Obj: obj,
	}
	if err == nil {
		m.Success = true
		if msg != "" {
			m.Msg = msg
		}
	} else {
		m.Success = false
		m.Msg = redact.StringLimit(msg+": "+err.Error(), MaxClientErrorBytes)
		if w.Warning != nil {
			w.Warning("failed :", err)
		}
	}
	c.JSON(http.StatusOK, m)
}

func PureJSONMsg(c *gin.Context, success bool, msg string) {
	if success {
		c.JSON(http.StatusOK, Msg{
			Success: true,
			Msg:     msg,
		})
	} else {
		c.JSON(http.StatusOK, Msg{
			Success: false,
			Msg:     msg,
		})
	}
}
