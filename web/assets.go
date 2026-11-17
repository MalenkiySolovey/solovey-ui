package web

import (
	owner "github.com/MalenkiySolovey/solovey-ui/backend/http/frontend"
	"github.com/gin-gonic/gin"
	"io/fs"
)

func serveAssetsFS(assets fs.FS) gin.HandlerFunc { return owner.ServeAssets(assets) }
