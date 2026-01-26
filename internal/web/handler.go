package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/likaia/nginxpulse/internal/analytics"
	"github.com/likaia/nginxpulse/internal/config"
	"github.com/likaia/nginxpulse/internal/enrich"
	"github.com/likaia/nginxpulse/internal/ingest"
	"github.com/likaia/nginxpulse/internal/version"
	"github.com/sirupsen/logrus"
)

// 初始化Web路由
func SetupRoutes(
	router *gin.Engine,
	statsFactory *analytics.StatsFactory,
	logParser *ingest.LogParser) {

	// 获取所有网站列表
	router.GET("/api/websites", func(c *gin.Context) {
		websiteIDs := config.GetAllWebsiteIDs()

		websites := make([]map[string]string, 0, len(websiteIDs))
		for _, id := range websiteIDs {
			website, ok := config.GetWebsiteByID(id)
			if !ok {
				continue
			}

			websites = append(websites, map[string]string{
				"id":   id,
				"name": website.Name,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"websites": websites,
		})
	})

	router.GET("/api/status", func(c *gin.Context) {
		cfg := config.ReadConfig()
		migrationRequired := needsPGMigration()
		ipGeoPendingCount := int64(0)
		if logParser != nil {
			ipGeoPendingCount = logParser.GetIPGeoPendingCount()
		}
		c.JSON(http.StatusOK, gin.H{
			"log_parsing":                             ingest.IsIPParsing(),
			"log_parsing_progress":                    ingest.GetIPParsingProgress(),
			"log_parsing_estimated_total_seconds":     ingest.GetIPParsingEstimatedTotalSeconds(),
			"log_parsing_estimated_remaining_seconds": ingest.GetIPParsingEstimatedRemainingSeconds(),
			"ip_geo_parsing":                          ingest.IsIPGeoParsing(),
			"ip_geo_pending":                          ipGeoPendingCount > 0,
			"ip_geo_progress":                         ingest.GetIPGeoParsingProgress(ipGeoPendingCount),
			"ip_geo_estimated_remaining_seconds":      ingest.GetIPGeoEstimatedRemainingSeconds(ipGeoPendingCount),
			"demo_mode":                               cfg.System.DemoMode,
			"language":                                config.NormalizeLanguage(cfg.System.Language),
			"version":                                 version.Version,
			"git_commit":                              version.GitCommit,
			"migration_required":                      migrationRequired,
			"setup_required":                          config.IsSetupMode(),
			"config_readonly":                         config.ConfigReadOnly(),
		})
	})

	router.GET("/api/config", func(c *gin.Context) {
		cfg, err := config.ReadRawConfig()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("读取配置失败: %v", err),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"config":         cfg,
			"readonly":       config.ConfigReadOnly(),
			"setup_required": config.IsSetupMode(),
		})
	})

	router.POST("/api/config/validate", func(c *gin.Context) {
		cfg, err := bindConfigPayload(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "请求参数错误",
			})
			return
		}
		result := config.ValidateConfig(cfg, config.ValidateOptions{
			CheckPaths: true,
		})
		c.JSON(http.StatusOK, result)
	})

	router.POST("/api/config/save", func(c *gin.Context) {
		if config.ConfigReadOnly() {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "配置来自环境变量，无法保存",
			})
			return
		}

		cfg, err := bindConfigPayload(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "请求参数错误",
			})
			return
		}

		result := config.ValidateConfig(cfg, config.ValidateOptions{
			CheckPaths: true,
		})
		if len(result.Errors) > 0 {
			c.JSON(http.StatusBadRequest, result)
			return
		}

		if err := config.WriteConfigFile(cfg); err != nil {
			logrus.WithError(err).Error("保存配置失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("保存配置失败: %v", err),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":          true,
			"restart_required": true,
		})
	})

	router.POST("/api/system/restart", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})

		go func() {
			time.Sleep(200 * time.Millisecond)
			if proc, err := os.FindProcess(os.Getpid()); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
		}()
	})

	router.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version":    version.Version,
			"git_commit": version.GitCommit,
		})
	})

	router.POST("/api/logs/reparse", func(c *gin.Context) {
		if logParser == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "初始化模式暂不支持日志解析",
			})
			return
		}
		type reparseRequest struct {
			ID        string `json:"id"`
			Migration bool   `json:"migration"`
		}

		var req reparseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "请求参数错误",
			})
			return
		}

		websiteID := strings.TrimSpace(req.ID)
		if websiteID != "" {
			if _, ok := config.GetWebsiteByID(websiteID); !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "站点不存在",
				})
				return
			}
		}

		if err := logParser.TriggerReparse(websiteID); err != nil {
			if errors.Is(err, ingest.ErrParsingInProgress) {
				c.JSON(http.StatusConflict, gin.H{
					"error": err.Error(),
				})
				return
			}
			logrus.WithError(err).Error("触发重新解析失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("重新解析失败: %v", err),
			})
			return
		}

		if req.Migration {
			if err := markPGMigrationDone(); err != nil {
				logrus.WithError(err).Warn("记录迁移状态失败")
			}
		}

		statsFactory.ClearCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	})

	router.GET("/api/ip-geo/anomaly", func(c *gin.Context) {
		if statsFactory == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "统计模块暂不可用",
			})
			return
		}
		websiteID := strings.TrimSpace(c.Query("id"))
		if websiteID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}
		if _, ok := config.GetWebsiteByID(websiteID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}
		count, samples, err := statsFactory.Repo().DetectIPGeoAnomalies(websiteID, 5)
		if err != nil {
			logrus.WithError(err).Error("检测 IP 归属地异常失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("检测失败: %v", err),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"has_issue": count > 0,
			"count":     count,
			"samples":   samples,
		})
	})

	router.POST("/api/ip-geo/repair", func(c *gin.Context) {
		if logParser == nil || statsFactory == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "初始化模式暂不支持日志解析",
			})
			return
		}
		type repairRequest struct {
			ID string `json:"id"`
		}
		var req repairRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "请求参数错误",
			})
			return
		}
		websiteID := strings.TrimSpace(req.ID)
		if websiteID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}
		if _, ok := config.GetWebsiteByID(websiteID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}

		repo := statsFactory.Repo()
		if err := repo.ClearIPGeoCache(); err != nil {
			logrus.WithError(err).Error("清空 IP 归属地缓存失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("清空 IP 归属地缓存失败: %v", err),
			})
			return
		}
		if err := repo.ClearIPGeoPending(); err != nil {
			logrus.WithError(err).Error("清空 IP 归属地待解析队列失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("清空 IP 归属地待解析队列失败: %v", err),
			})
			return
		}
		enrich.ResetIPGeoCache()

		if err := logParser.TriggerReparse(websiteID); err != nil {
			if errors.Is(err, ingest.ErrParsingInProgress) {
				c.JSON(http.StatusConflict, gin.H{
					"error": err.Error(),
				})
				return
			}
			logrus.WithError(err).Error("触发重新解析失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("重新解析失败: %v", err),
			})
			return
		}
		statsFactory.ClearCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	})

	router.GET("/api/logs/export", func(c *gin.Context) {
		if statsFactory == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "初始化模式暂不支持日志导出",
			})
			return
		}

		params := map[string]string{
			"page":      "1",
			"pageSize":  fmt.Sprintf("%d", exportBatchSize),
			"sortField": "timestamp",
			"sortOrder": "desc",
		}
		for key, values := range c.Request.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}

		query, err := statsFactory.BuildQueryFromRequest("logs", params)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		if _, ok := config.GetWebsiteByID(query.WebsiteID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}

		filename := fmt.Sprintf("nginxpulse_logs_%s.csv", time.Now().Format("20060102_150405"))
		c.Header("Content-Type", csvContentType)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)

		if err := exportLogsCSV(c.Writer, statsFactory, query, c.Query("lang")); err != nil {
			logrus.WithError(err).Error("导出日志失败")
		}
	})

	router.POST("/api/ingest/logs", func(c *gin.Context) {
		if logParser == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "初始化模式暂不支持日志解析",
			})
			return
		}
		type ingestRequest struct {
			WebsiteID string   `json:"website_id"`
			SourceID  string   `json:"source_id"`
			Lines     []string `json:"lines"`
		}

		var req ingestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "请求参数错误",
			})
			return
		}

		websiteID := strings.TrimSpace(req.WebsiteID)
		if websiteID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "缺少站点ID",
			})
			return
		}
		if _, ok := config.GetWebsiteByID(websiteID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "站点不存在",
			})
			return
		}
		if len(req.Lines) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "日志内容为空",
			})
			return
		}

		accepted, deduped, err := logParser.IngestLines(websiteID, strings.TrimSpace(req.SourceID), req.Lines)
		if err != nil {
			logrus.WithError(err).Error("日志推送解析失败")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("解析失败: %v", err),
			})
			return
		}

		statsFactory.ClearCache()
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"accepted": accepted,
			"deduped":  deduped,
		})
	})

	// 查询接口
	router.GET("/api/stats/:type", func(c *gin.Context) {
		if statsFactory == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "初始化模式暂不支持统计查询",
			})
			return
		}
		statsType := c.Param("type")
		params := make(map[string]string)
		for key, values := range c.Request.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}

		query, err := statsFactory.BuildQueryFromRequest(statsType, params)

		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		// 执行查询
		result, err := statsFactory.QueryStats(statsType, query)
		if err != nil {
			logrus.WithError(err).Errorf("查询统计数据[%s]失败", statsType)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("查询失败: %v", err),
			})
			return
		}

		c.JSON(http.StatusOK, result)
	})

}

func bindConfigPayload(c *gin.Context) (*config.Config, error) {
	payload := struct {
		Config config.Config `json:"config"`
	}{
		Config: config.DefaultConfig(),
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		return nil, err
	}
	return &payload.Config, nil
}

func migrationMarkerPath() string {
	return filepath.Join(config.DataDir, "pg_migration_done")
}

func sqliteDataPath() string {
	return filepath.Join(config.DataDir, "nginxpulse.db")
}

func needsPGMigration() bool {
	if _, err := os.Stat(migrationMarkerPath()); err == nil {
		return false
	}
	if _, err := os.Stat(sqliteDataPath()); err == nil {
		return true
	}
	return false
}

func markPGMigrationDone() error {
	if err := os.WriteFile(migrationMarkerPath(), []byte("ok\n"), 0644); err != nil {
		return err
	}
	if err := os.Remove(sqliteDataPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
