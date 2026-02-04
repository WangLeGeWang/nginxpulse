package server

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/likaia/nginxpulse/internal/config"
)

func attachAppConfig(router *gin.Engine) {
	router.GET("/app-config.js", func(c *gin.Context) {
		base := config.NormalizeWebBasePath(config.ReadConfig().System.WebBasePath)
		prefix := ""
		if base != "" {
			prefix = "/" + base
		}
		payload, _ := json.Marshal(prefix)
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, "window.__NGINXPULSE_BASE_PATH__ = %s;", payload)
	})
}
