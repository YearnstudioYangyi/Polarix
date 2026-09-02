package admin

import (
	"Plrx/lib/assets"
	"Plrx/lib/config"
	"Plrx/lib/plugin"
	"crypto/subtle"
	_ "embed"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed panel.html
var panelPage []byte

//go:embed plugin.html
var pluginPage []byte

//go:embed assets.html
var assetsPage []byte

// Register 注册管理路由。assetsMgr 为 nil 时跳过图床管理路由。
func Register(router *gin.Engine, password string, assetsMgr *assets.Manager) {
	admin := router.Group("/admin")
	admin.Use(access(password))
	admin.GET("", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", panelPage)
	})
	admin.GET("/plugins/:id", func(c *gin.Context) {
		if _, ok := plugin.ManagedPluginByID(c.Param("id")); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", pluginPage)
	})
	admin.GET("/api/plugins", func(c *gin.Context) {
		c.JSON(http.StatusOK, plugin.ManagedPlugins())
	})
	admin.GET("/api/plugins/:id", func(c *gin.Context) {
		managed, ok := plugin.ManagedPluginByID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "插件不存在"})
			return
		}
		c.JSON(http.StatusOK, managed)
	})
	admin.PUT("/api/plugins/:id", func(c *gin.Context) {
		var input map[string]any
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
			return
		}
		prepared, err := plugin.PrepareConfiguration(c.Param("id"), input)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := config.SavePluginSettings(c.Param("id"), prepared); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
			return
		}
		if err := plugin.ApplyConfiguration(c.Param("id"), prepared); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	admin.PUT("/api/plugins/:id/access", func(c *gin.Context) {
		var input plugin.AccessConfig
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
			return
		}
		prepared, err := plugin.PrepareAccessConfiguration(c.Param("id"), input)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		persisted := config.AccessConfig{
			Default:  toConfigAccessRule(prepared.Default),
			Commands: make(map[string]config.AccessRule, len(prepared.Commands)),
		}
		for path, rule := range prepared.Commands {
			persisted.Commands[path] = toConfigAccessRule(rule)
		}
		if err := config.SavePluginAccess(c.Param("id"), persisted); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存访问控制失败"})
			return
		}
		plugin.ApplyAccessConfiguration(c.Param("id"), prepared)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	if assetsMgr == nil {
		return
	}
	admin.GET("/assets", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", assetsPage)
	})
	admin.GET("/api/assets", func(c *gin.Context) {
		c.JSON(http.StatusOK, buildAssetsView(assetsMgr))
	})
	admin.PUT("/api/assets", func(c *gin.Context) {
		var input assetsConfigInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
			return
		}
		cfg, err := resolveAssetsConfig(assetsMgr, input)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := assetsMgr.Save(cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}

// assetsConfigInput PUT /admin/api/assets 请求体。
type assetsConfigInput struct {
	Whitelist []string              `json:"whitelist"`
	Providers []assetsInputProvider `json:"providers"`
}

// assetsInputProvider 单个 provider 的保存载荷。
type assetsInputProvider struct {
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

// assetsViewProvider GET /admin/api/assets 返回的单个 provider 视图。
type assetsViewProvider struct {
	Name       string               `json:"name"`
	Enabled    bool                 `json:"enabled"`
	Priority   int                  `json:"priority"`
	Configured bool                 `json:"configured"`
	HasSecrets bool                 `json:"has_secrets"`
	Schema     []assets.ConfigField `json:"schema"`
	Config     map[string]any       `json:"config"`
}

// buildAssetsView 合并注册表与磁盘配置，生成管理页视图（密码字段掩码为空）。
func buildAssetsView(mgr *assets.Manager) gin.H {
	cfg := mgr.Config()
	stored := make(map[string]assets.ProviderItem, len(cfg.Providers))
	for _, item := range cfg.Providers {
		stored[item.Name] = item
	}

	providers := make([]assetsViewProvider, 0, len(assets.Names()))
	for _, name := range assets.Names() {
		schema := assets.ProviderSchema(name)
		item, ok := stored[name]
		enabled := ok && (item.Enabled == nil || *item.Enabled)
		priority := 0
		var config map[string]any
		hasSecrets := false
		if ok {
			priority = item.Priority
			config = maskPasswords(schema, item.Config)
			for _, f := range schema {
				if f.Type == "password" {
					if v, exists := item.Config[f.Key]; exists && v != "" && v != nil {
						hasSecrets = true
						break
					}
				}
			}
		} else {
			config = make(map[string]any)
		}
		providers = append(providers, assetsViewProvider{
			Name:       name,
			Enabled:    enabled,
			Priority:   priority,
			Configured: ok,
			HasSecrets: hasSecrets,
			Schema:     schema,
			Config:     config,
		})
	}
	whitelist := cfg.Whitelist
	if whitelist == nil {
		whitelist = []string{}
	}
	return gin.H{
		"whitelist": whitelist,
		"providers": providers,
	}
}

// resolveAssetsConfig 将请求载荷落成 HostConfig：密码字段留空时保留旧值。
func resolveAssetsConfig(mgr *assets.Manager, input assetsConfigInput) (assets.HostConfig, error) {
	oldCfg := mgr.Config()
	old := make(map[string]assets.ProviderItem, len(oldCfg.Providers))
	for _, item := range oldCfg.Providers {
		old[item.Name] = item
	}

	providers := make([]assets.ProviderItem, 0, len(input.Providers))
	for _, item := range input.Providers {
		var oldConfig map[string]any
		if prev, ok := old[item.Name]; ok {
			oldConfig = prev.Config
		}
		enabled := item.Enabled
		providers = append(providers, assets.ProviderItem{
			Name:     item.Name,
			Enabled:  &enabled,
			Priority: item.Priority,
			Config:   mergePasswordFields(assets.ProviderSchema(item.Name), oldConfig, item.Config),
		})
	}
	return assets.HostConfig{
		Whitelist: input.Whitelist,
		Providers: providers,
	}, nil
}

// maskPasswords 把 schema 中 type=password 的字段值置空，避免密钥回显。
func maskPasswords(schema []assets.ConfigField, cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	result := make(map[string]any, len(cfg))
	for k, v := range cfg {
		result[k] = v
	}
	for _, f := range schema {
		if f.Type == "password" {
			result[f.Key] = ""
		}
	}
	return result
}

// mergePasswordFields 对 schema 中 type=password 的字段：新值为空时沿用旧值。
func mergePasswordFields(schema []assets.ConfigField, old, next map[string]any) map[string]any {
	if next == nil {
		next = make(map[string]any)
	}
	for _, f := range schema {
		if f.Type != "password" {
			continue
		}
		v, exists := next[f.Key]
		if (exists && (v == nil || v == "")) || !exists {
			if old != nil {
				if oldVal, has := old[f.Key]; has {
					next[f.Key] = oldVal
				}
			}
		}
	}
	return next
}

func toConfigAccessRule(rule plugin.AccessRule) config.AccessRule {
	return config.AccessRule{Mode: rule.Mode, Users: rule.Users, Groups: rule.Groups}
}

func access(password string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if password == "" {
			host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
			if err == nil && net.ParseIP(host).IsLoopback() {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "远程管理未启用，请在 config.json 中设置 admin_password"})
			return
		}
		username, provided, ok := c.Request.BasicAuth()
		validUser := subtle.ConstantTimeCompare([]byte(username), []byte("admin")) == 1
		validPassword := subtle.ConstantTimeCompare([]byte(provided), []byte(password)) == 1
		if !ok || !validUser || !validPassword {
			c.Header("WWW-Authenticate", `Basic realm="Bot admin", charset="UTF-8"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
