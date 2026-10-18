package main

import (
	"Plrx/lib/admin"
	"Plrx/lib/config"
	"Plrx/lib/constant"
	appcontext "Plrx/lib/context"
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
	"strings"

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
	plugins_push.SetClient(&client)
	schedule.Start(&client)

	r, wsClient := setupRouter(appConfig, &client)
	if wsClient != nil {
		go wsClient.Start()
	}

	log.Printf("Server running on %v", appConfig.Port)
	log.Printf("注册了%v个Markdown模板", templates.GetMarkdownTemplateCount())
	log.Printf("注册了%v个指令", plugin.GetCommandCount())
	log.Printf("注册了%v个定时任务", schedule.GetJobCount())
	r.Run(fmt.Sprintf(":%v", appConfig.Port))
}

func setupRouter(appConfig config.AppConfig, client *qqapi.Client) (*gin.Engine, *gateway.Client) {
	r := gin.Default()
	admin.Register(r, appConfig.AdminPassword)
	r.Any("/_plugin/:plugin/:token", func(c *gin.Context) {
		appcontext.ServeTemporaryRoute(c.Writer, c.Request, c.Param("plugin"), c.Param("token"))
	})

	// 主动推送接口 (不经过QQ签名校验)
	r.POST("/push/:scope/:openid", plugins_push.HTTPHandle)

	mode := strings.ToLower(strings.TrimSpace(appConfig.Mode))
	if mode == "" {
		mode = "webhook"
	}

	// 模式 1: Webhook 接收事件
	if mode == "webhook" || mode == "both" {
		log.Printf("[模式] 启用 WebHook 监听 (/webhook)")
		webhook := r.Group("/")
		webhook.Use(middleware.VerifySignature(appConfig.AppSecret))
		webhook.POST("/webhook", func(c *gin.Context) {
			c.Status(http.StatusOK)
			var payload structers.Payload
			if err := c.ShouldBindJSON(&payload); err != nil {
				return
			}
			// Op = 13, 签名验证
			if payload.Op == 13 {
				seed := appConfig.AppSecret
				for len(seed) < ed25519.SeedSize {
					seed = strings.Repeat(seed, 2)
				}
				reader := strings.NewReader(seed[:ed25519.SeedSize])
				_, privateKey, _ := ed25519.GenerateKey(reader)

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
			go middleware.ProcessPayload(payload, client)
		})
	}

	var wsClient *gateway.Client
	// 模式 2: WebSocket 长连接接收事件
	if mode == "websocket" || mode == "both" {
		log.Printf("[模式] 启用 WebSocket 网关客户端")
		wsClient = gateway.NewClient(gateway.Config{
			Intents: appConfig.WebSocket.Intents,
			Shard:   appConfig.WebSocket.Shard,
			Gateway: appConfig.WebSocket.Gateway,
		}, client)
	}

	return r, wsClient
}
