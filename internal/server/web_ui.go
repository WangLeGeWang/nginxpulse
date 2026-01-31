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
		if isMobileUserAgent(c.Request.UserAgent()) && requestPath != "/m" && !strings.HasPrefix(requestPath, "/m/") {
			target := "/m" + requestPath
			if requestPath == "/" {
				target = "/m/"
			}
			if c.Request.URL.RawQuery != "" {
				target = target + "?" + c.Request.URL.RawQuery
			}
			c.Redirect(http.StatusFound, target)
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

func isMobileUserAgent(ua string) bool {
	ua = strings.ToLower(ua)
	if ua == "" {
		return false
	}
	return strings.Contains(ua, "android") ||
		strings.Contains(ua, "iphone") ||
		strings.Contains(ua, "ipad") ||
		strings.Contains(ua, "ipod") ||
		strings.Contains(ua, "mobile") ||
		strings.Contains(ua, "windows phone") ||
		strings.Contains(ua, "blackberry") ||
		strings.Contains(ua, "opera mini") ||
		strings.Contains(ua, "mobi")
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
