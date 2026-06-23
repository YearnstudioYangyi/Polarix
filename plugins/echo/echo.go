package echo

import (
	"Plrx/lib/constant"
	"Plrx/lib/constant/Button/ActionPermissionType"
	"Plrx/lib/constant/Button/ButtonStyle"
	"Plrx/lib/context"
	"Plrx/lib/plugin"
	"Plrx/lib/structers"
	"Plrx/lib/structers/buttons"
	"Plrx/lib/templates"
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

	commands = append(commands, &plugin.Command{
		Prefix:    "/uid",
		Role:      constant.RoleMember,
		Describle: "获取用户ID",
		Handle:    getUserId,
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

func getUserId(ctx *context.Context) error {
	tmp, err := templates.FillMarkdownTemplate("UserIdCard", templates.Args{
		"id":       ctx.Message.UserId,
		"union_id": ctx.Message.UnionId,
		"msg_id":   ctx.Message.MessageId,
	})
	if err != nil {
		return err
	}
	// ctx.Reply(tmp, structers.Markdown)
	keyboard := &buttons.Keyboard{}
	button, err := keyboard.AppendButton("1", "查询我的", "查询我的", ButtonStyle.Blue, 0)
	if err != nil {
		return err
	}
	button.SetAutoCommand("/uid", true, false)
	button.SetPermission(ActionPermissionType.AllUser)
	button.SetUnsupportedTip("不支持按钮")
	msg := structers.Message{
		Content:     tmp,
		MessageId:   ctx.Message.MessageId,
		GroupId:     ctx.Message.GroupId,
		Keyboard:    *keyboard,
		MessageType: structers.Markdown,
	}
	_, err = buttons.GenerateJson(msg.Keyboard)
	// log.Printf("[Debug]生成的JSON: %v", string(a))
	if err != nil {
		return err
	}
	return ctx.Client.SendGroupMessage(msg, msg.GroupId)
	// return nil
}
