package echo

import (
	"botOffical/lib/constant"
	"botOffical/lib/context"
	"botOffical/lib/plugin"
	"botOffical/lib/structers"
	"fmt"
)

func init() {
	// fmt.Println("Echo插件初始化")
	var commands []*plugin.Command
	commands = append(commands, &plugin.Command{
		Prefix:    "/echo",
		Role:      constant.RoleMember,
		Describle: "回显",
		Handle:    echoHandle,
	})

	commands = append(commands, &plugin.Command{
		Prefix:    "/groupid",
		Role:      constant.RoleMember,
		Describle: "获取群OpenID",
		Handle:    getGroupId,
	})

	self := plugin.PluginConfig{
		Id:       "echo",
		Commands: commands,
	}
	plugin.Register(&self)
}

func echoHandle(ctx *context.Context) error {
	// log.Printf("传入到处理函数, 消息内容: %v, 来源群: %v", ctx.Message.Content, ctx.Message.GroupId)
	return ctx.Client.SendGroupMessage(*ctx.Message, ctx.Message.GroupId)
	// return nil
}

func getGroupId(ctx *context.Context) error {
	msg := structers.Message{
		Content:     fmt.Sprintf("##获取群ID结果\n```\n%v\n```", ctx.Message.GroupId),
		MessageType: structers.Markdown,
	}
	return ctx.Client.SendGroupMessage(msg, ctx.Message.GroupId)
}
