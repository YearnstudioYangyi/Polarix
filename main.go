package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"

	"botOffical/lib/constant"
	"botOffical/lib/context"
	"botOffical/lib/plugin"
	"botOffical/lib/qqapi"
	"botOffical/lib/structers"
	_ "botOffical/plugins"

	"github.com/gin-gonic/gin"
)

// 通用 Payload 结构
type Payload struct {
	ID   string      `json:"id"`
	Op   int         `json:"op"`
	Data MessageData `json:"d"`
	T    string      `json:"t"`
}

type MessageData struct {
	Id          string `json:"id"`
	Content     string `json:"content"`
	GroupOpenID string `json:"group_openid"`
	Author      struct {
		UnionID  string            `json:"union_openid"`
		Role     constant.UserRole `json:"member_role"`
		Username string            `json:"username"`
		MemberId string
	} `json:"author"`
}

func initConfig() structers.AppConfig {
	// 解析配置
	file, err := os.ReadFile("./config.json")
	if err != nil {
		fmt.Println("请正确配置config.json")
		os.Exit(1)
	}

	var appConfig structers.AppConfig
	err = json.Unmarshal(file, &appConfig)
	if err != nil {
		fmt.Println("请正确配置config.json")
		os.Exit(1)
	}
	return appConfig
}

func main() {
	appConfig := initConfig()

	r := gin.Default()
	client := qqapi.Init(appConfig.AppId, appConfig.AppSecret, appConfig.ProxyAPI)
	r.POST("/webhook", func(c *gin.Context) {
		// 获取原始 Body
		bodyBytes, _ := io.ReadAll(c.Request.Body)
		// log.Printf("[DEBUG RAW] %s", string(bodyBytes))

		// 重新填充 Body 供后续解析
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		var payload Payload
		if err := c.ShouldBindJSON(&payload); err != nil {
			return
		}

		switch payload.T {
		case "GROUP_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE":
			// 群消息
			payload.Data.Content = strings.TrimSpace(payload.Data.Content)
			// log.Printf("消息内容: %v\n发送群: %v  发送者:%v (权限级别: %v)", payload.Data.Content, payload.Data.GroupOpenID, payload.Data.Author.UnionID, payload.Data.Author.Role)
			msgs := strings.Split(payload.Data.Content, " ")
			var prefix = msgs[0]
			// 处理可能的@前缀
			if len(msgs) > 1 && (strings.HasPrefix(msgs[0], "\u003c@") || strings.HasPrefix(msgs[0], "<@")) && (strings.HasSuffix(msgs[0], ">")) {
				// msgs = strings.Split(payload.Data.Content, " ")[1]
				prefix = msgs[1]
			}
			// log.Printf("")
			cmd, ok := plugin.GetCommand(prefix)
			if ok {
				if !cmd.Role.CanUse(payload.Data.Author.Role) {
					log.Printf("用户%v无权限使用%v指令, 最低要求权限: %v, 用户权限: %v", payload.Data.Author.Username, cmd.Prefix, cmd.Role, payload.Data.Author.Role)
					break
				}
				log.Printf("匹配到指令: %v, 来自插件: %v", cmd.Prefix, cmd.PluginId)
				var parsed any
				if cmd.ParserTarget != nil {
					// 注册了接收模板
					result := reflect.New(cmd.ParserTarget)
					err := cmd.Parser.Parse(payload.Data.Content, result.Interface())
					if err != nil {
						log.Printf("[Error:PluginParser:%v]: %v", cmd.PluginId, err)
						break
					}
					parsed = result.Interface()
				} else {
					// 采用默认模板
					var result string
					err := cmd.Parser.Parse(payload.Data.Content, &result)
					if err != nil {
						log.Printf("[Error:PluginParser:%v]: %v", cmd.PluginId, err)
						break
					}
					parsed = result
				}

				ctx := context.Context{
					Client: &client,
					Message: &structers.Message{
						Content:     payload.Data.Content,
						MessageType: structers.PlainText,
						MessageFrom: structers.GroupMessage,
						UserId:      payload.Data.Author.MemberId,
						UnionId:     payload.Data.Author.UnionID,
						GroupId:     payload.Data.GroupOpenID,
						MessageId:   payload.Data.Id,
					},
					Parserd: parsed,
				}
				err := cmd.Handle(&ctx)
				if err != nil {
					log.Printf("[Error:Plugin:%v]: %v", cmd.PluginId, err)
					break
				}
			}
		}
		c.Status(http.StatusOK)
		return
	})

	log.Printf("Server running on %v", appConfig.Port)
	r.Run(fmt.Sprintf(":%v", appConfig.Port))
}

// 官方要求的签名校验逻辑
// func handleValidation(c *gin.Context, data json.PrefixMessage) {
// 	var v struct {
// 		PlainToken string `json:"plain_token"`
// 		EventTs    string `json:"event_ts"`
// 	}
// 	json.Unmarshal(data, &v)

// 	seed := BotSecret
// 	for len(seed) < ed25519.SeedSize {
// 		seed = strings.Repeat(seed, 2)
// 	}
// 	seed = seed[:ed25519.SeedSize]
// 	reader := strings.NewReader(seed)
// 	_, privateKey, _ := ed25519.GenerateKey(reader)

// 	var msg bytes.Buffer
// 	msg.WriteString(v.EventTs)
// 	msg.WriteString(v.PlainToken)

// 	signature := hex.EncodeToString(ed25519.Sign(privateKey, msg.Bytes()))
// 	c.JSON(http.StatusOK, gin.H{
// 		"plain_token": v.PlainToken,
// 		"signature":   signature,
// 	})
// }
