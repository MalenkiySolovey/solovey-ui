package api

import (
	"github.com/MalenkiySolovey/solovey-backend/http/response"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/gin-gonic/gin"
)

type Msg = response.Msg

func jsonMsg(c *gin.Context, msg string, err error) {
	response.Writer{Warning: logger.Warning}.JSONMsg(c, msg, err)
}
func jsonObj(c *gin.Context, obj interface{}, err error) {
	response.Writer{Warning: logger.Warning}.JSONObj(c, obj, err)
}
func jsonMsgObj(c *gin.Context, msg string, obj interface{}, err error) {
	response.Writer{Warning: logger.Warning}.JSONMsgObj(c, msg, obj, err)
}
func pureJsonMsg(c *gin.Context, success bool, msg string) { response.PureJSONMsg(c, success, msg) }
