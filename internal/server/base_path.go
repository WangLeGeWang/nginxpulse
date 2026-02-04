package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/likaia/nginxpulse/internal/config"
)

func basePathMiddleware() gin.HandlerFunc {
	prefix := config.WebBasePathPrefix()
	if prefix == "" {
		return func(c *gin.Context) {
			c.Next()
		}
	}
	prefixWithSlash := prefix + "/"

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == prefix {
			c.Redirect(http.StatusFound, prefixWithSlash)
			c.Abort()
			return
		}
		if strings.HasPrefix(path, prefixWithSlash) {
			stripped := strings.TrimPrefix(path, prefix)
			if stripped == "" {
				stripped = "/"
			}
			c.Request.URL.Path = stripped
			c.Next()
			return
		}
		if isSharedAssetPath(path) {
			c.Next()
			return
		}
		c.Status(http.StatusNotFound)
		c.Abort()
	}
}

func isSharedAssetPath(path string) bool {
	switch path {
	case "/app-config.js", "/favicon.svg", "/brand-mark.svg":
		return true
	}
	return strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/m/assets/")
}
