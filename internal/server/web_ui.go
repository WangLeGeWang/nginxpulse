package server

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/likaia/nginxpulse/internal/webui"
	"github.com/sirupsen/logrus"
)

func attachWebUI(router *gin.Engine) {
	assets, ok := webui.AssetFS()
	if !ok {
		logrus.Info("未检测到内置前端资源，跳过静态页面服务")
		return
	}
	mobileAssets, mobileOk := webui.MobileAssetFS()
	if !mobileOk {
		logrus.Info("未检测到内置移动端资源，/m 将无法访问")
	}

	fileServer := http.FileServer(http.FS(assets))
	var mobileFileServer http.Handler
	if mobileOk {
		mobileFileServer = http.FileServer(http.FS(mobileAssets))
	}

	serveStatic := func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		requestPath := c.Request.URL.Path
		if strings.HasPrefix(requestPath, "/api/") || requestPath == "/api" || strings.HasPrefix(requestPath, "/m/api/") || requestPath == "/m/api" {
			c.Status(http.StatusNotFound)
			return
		}
		isMobile := requestPath == "/m" || strings.HasPrefix(requestPath, "/m/")
		if isMobile {
			if !mobileOk {
				c.Status(http.StatusNotFound)
				return
			}
			mobilePath := strings.TrimPrefix(requestPath, "/m")
			serveStaticFromFS(mobileAssets, mobileFileServer, mobilePath, c)
			return
		}

		serveStaticFromFS(assets, fileServer, requestPath, c)
	}

	router.NoRoute(serveStatic)
}

func serveStaticFromFS(assets fs.FS, fileServer http.Handler, requestPath string, c *gin.Context) {
	cleanPath := path.Clean("/" + requestPath)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if cleanPath == "" || cleanPath == "index.html" {
		serveIndex(assets, c)
		return
	}

	if _, err := fs.Stat(assets, cleanPath); err == nil {
		c.Request.URL.Path = "/" + cleanPath
		fileServer.ServeHTTP(c.Writer, c.Request)
		return
	}

	baseName := path.Base(cleanPath)
	isAsset := strings.HasPrefix(cleanPath, "assets/") || strings.Contains(baseName, ".")
	if isAsset {
		c.Status(http.StatusNotFound)
		return
	}

	serveIndex(assets, c)
}

func serveIndex(assets fs.FS, c *gin.Context) {
	indexPath := "index.html"
	if _, err := fs.Stat(assets, indexPath); err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if file, err := assets.Open(indexPath); err == nil {
		defer file.Close()
		_, _ = io.Copy(c.Writer, file)
	} else {
		c.Status(http.StatusNotFound)
	}
}
