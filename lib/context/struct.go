package context

import (
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"Plrx/lib/structers"
)

type Context struct {
	Client   *qqapi.Client      // QQ API对象
	Message  *structers.Message // 原始消息结构体
	Parserd  any                // 解析后的字段
	Requests *requests.Client   // 公共请求代理
}

func (ctx *Context) Reply(content string, msgType structers.MessageType) error {
	if ctx.Message.GroupId == "" {
		msg := structers.Message{
			Content:     content,
			MessageType: msgType,
			MessageId:   ctx.Message.MessageId,
		}
		ctx.Message.MessageId = ""
		return ctx.Client.SendPrivateMessage(msg, ctx.Message.UserId)
	} else {
		msg := structers.Message{
			Content:     content,
			MessageType: msgType,
			MessageId:   ctx.Message.MessageId,
		}
		ctx.Message.MessageId = ""
		return ctx.Client.SendGroupMessage(msg, ctx.Message.GroupId)
	}
}
