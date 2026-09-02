package main

import (
	"Plrx/lib/admin"
	"Plrx/lib/assets"
	_ "Plrx/lib/assets/providers"
	"Plrx/lib/config"
	"Plrx/lib/constant"
	"Plrx/lib/gateway"
	"Plrx/lib/middleware"
	"Plrx/lib/plugin"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"Plrx/lib/schedule"
	"Plrx/lib/storage"
	"Plrx/lib/structers"
	"Plrx/lib/templates"
	_ "Plrx/plugins"
	plugins_push "Plrx/plugins/push"
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

var requestsClient *requests.Client = requests.Init(5)

func main() {
	// 初始化相关配置
	appConfig := config.InitConfig()
	if err := plugin.LoadConfigurations(appConfig.PluginSettings); err != nil {
		log.Fatalf("Failed to load plugin configuration: %v", err)
	}
	accessConfigs := make(map[string]plugin.AccessConfig, len(appConfig.PluginAccess))
	for id, access := range appConfig.PluginAccess {
		commands := make(map[string]plugin.AccessRule, len(access.Commands))
		for path, rule := range access.Commands {
			commands[path] = plugin.AccessRule{Mode: rule.Mode, Users: rule.Users, Groups: rule.Groups}
		}
		accessConfigs[id] = plugin.AccessConfig{
			Default:  plugin.AccessRule{Mode: access.Default.Mode, Users: access.Default.Users, Groups: access.Default.Groups},
			Commands: commands,
		}
	}
	if err := plugin.LoadAccessConfigurations(accessConfigs); err != nil {
		log.Fatalf("Failed to load plugin access configuration: %v", err)
	}
	if err := storage.Open(appConfig.Database); err != nil {
		log.Fatalf("Failed to initialize SQLite storage: %v", err)
	}
	defer func() {
		if err := storage.Close(); err != nil {
			log.Printf("Failed to close SQLite storage: %v", err)
		}
	}()
	client := qqapi.Init(appConfig.AppId, appConfig.AppSecret, appConfig.ProxyAPI, requestsClient)

	// 图床聚合器：配置独立存放于 assets.json（不入 git，文件缺失视为未启用）
	assetsClient := assets.NewClient(30)
	assetsManager := assets.NewManager(assetsClient)
	assetsManager.OnReload(client.SetAssets)
	if err := assetsManager.Load(); err != nil {
		log.Printf("读取 assets.json 失败，使用空图床配置: %v", err)
	}
	if host := assetsManager.Host(); host.Size() > 0 {
		log.Printf("已启用图床聚合，provider 数量: %d", host.Size())
	}
	client.SetMessageOptions(appConfig.GlobalMarkdown, appConfig.MarkdownVerifyImage, appConfig.RetryWhen, appConfig.UploadThreshold)

	plugins_push.SetClient(&client)
	schedule.Start(&client)

	// 协议分发：webhook 起 HTTP 服务；websocket 起网关长连接
	if appConfig.Protocol == "websocket" {
		runGateway(&client, appConfig, assetsManager)
		return
	}

	r := gin.Default()
	admin.Register(r, appConfig.AdminPassword, assetsManager)

	// 主动推送接口 (不经过QQ签名校验)
	r.POST("/push/:scope/:openid", plugins_push.HTTPHandle)

	// 仅 webhook 需要QQ签名校验
	webhook := r.Group("/")
	webhook.Use(middleware.VerifySignature(appConfig.AppSecret))
	webhook.POST("/webhook", func(c *gin.Context) {
		c.Status(http.StatusOK)
		// 中间件已提取
		var payload structers.Payload
		if err := c.ShouldBindJSON(&payload); err != nil {
			return
		}
		// Op = 13, 签名验证
		if payload.Op == 13 {
			_, privateKey := middleware.DeriveEd25519Key(appConfig.AppSecret)

			var msg bytes.Buffer
			msg.WriteString(payload.Data.EventTs)
			msg.WriteString(payload.Data.PlainToken)

			signature := hex.EncodeToString(ed25519.Sign(privateKey, msg.Bytes()))
			c.JSON(http.StatusOK, gin.H{
				"plain_token": payload.Data.PlainToken,
				"signature":   signature,
			})
			return
		}

		if !constant.IsValidEventType(payload.T) {
			return
		}
		payload.EventType = constant.EventType(payload.T)
		go middleware.ProcessPayload(payload, &client)
	})

	log.Printf("Server running on %v", appConfig.Port)
	log.Printf("注册了%v个Markdown模板", templates.GetMarkdownTemplateCount())
	log.Printf("注册了%v个指令", plugin.GetCommandCount())
	log.Printf("注册了%v个定时任务", schedule.GetJobCount())
	r.Run(fmt.Sprintf(":%v", appConfig.Port))
}

// runGateway WebSocket 模式：起网关长连接 + admin/push HTTP。
func runGateway(client *qqapi.Client, appConfig config.AppConfig, assetsManager *assets.Manager) {
	gatewayURL, err := client.GatewayURL()
	if err != nil {
		log.Fatalf("获取网关地址失败: %v", err)
	}
	intents := gateway.Intents(appConfig.Intents)
	gw := gateway.New(client, gatewayURL, intents, [2]int{0, 1})
	go gw.Start()

	r := gin.Default()
	admin.Register(r, appConfig.AdminPassword, assetsManager)
	r.POST("/push/:scope/:openid", plugins_push.HTTPHandle)
	log.Printf("WebSocket 网关已启动（intents=%d），admin 与 push 接口仍在 :%d", intents, appConfig.Port)
	log.Printf("注册了%v个指令", plugin.GetCommandCount())
	log.Printf("注册了%v个定时任务", schedule.GetJobCount())
	r.Run(fmt.Sprintf(":%v", appConfig.Port))
}
