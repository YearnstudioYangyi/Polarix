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

	commands = append(commands, &plugin.Command{
		Prefix:    "/授权",
		Role:      constant.RoleMember,
		Describle: "获取群聊授权",
		Handle:    getPermission,
	})

	commands = append(commands, &plugin.Command{
		Prefix:    "权限申请引导",
		Role:      constant.RoleMember,
		Describle: "引导获取群聊授权",
		Handle:    getPermission,
	})

	self := plugin.PluginConfig{
		Id:       "echo",
		Commands: commands,
	}
	plugin.Register(&self)
}

func echoHandle(ctx *context.Context) error {
	return ctx.Reply("## 测试", structers.Markdown)
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
	button, err := keyboard.AppendButton("1", "测试", "测试", ButtonStyle.Blue, 0)
	if err != nil {
		return err
	}
	button.SetHref("https://club.vip.qq.com/transfer?open_kuikly_info=%7B%22page_name%22%3A%20%22ai_group_service_agreement_pop_page%22%2C%22groupCode%22%3A{%v}%2C%22botUin%22%3A{%v}%2C%22botUid%22%3A%22{%v}%22%2C%22screen%22%3A1%7D").SetPermission(ActionPermissionType.AllUser).SetUnsupportedTip("不支持按钮")

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

func getPermission(ctx *context.Context) error {
	tmp, err := templates.FillMarkdownTemplate("ReplyPermission", templates.Args{})
	if err != nil {
		return err
	}
	// ctx.Reply(tmp, structers.Markdown)
	keyboard := &buttons.Keyboard{}
	button, err := keyboard.AppendButton("1", "测试", "测试", ButtonStyle.Blue, 0)
	if err != nil {
		return err
	}
	button.SetHref("https://club.vip.qq.com/transfer?open_kuikly_info=%7B%22page_name%22%3A%20%22ai_group_service_agreement_pop_page%22%2C%22groupCode%22%3A{%v}%2C%22botUin%22%3A{%v}%2C%22botUid%22%3A%22{%v}%22%2C%22screen%22%3A1%7D").SetPermission(ActionPermissionType.AllUser).SetUnsupportedTip("不支持按钮")

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

func guideReply(ctx *context.Context) error {
	tmp, err := templates.FillMarkdownTemplate("GuideReply", templates.Args{})
	if err != nil {
		return err
	}
	keyboard := &buttons.Keyboard{}
	button, _ := keyboard.AppendButton("1", "点击输入群号", "请输入群号", ButtonStyle.Blue, 0)
	button.SetAutoCommand("<@>")
}
