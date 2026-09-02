package group

import (
	"Plrx/lib/buttons"
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/email"
	"Plrx/lib/plugin"
	"Plrx/lib/templates"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"
)

var mailRe *regexp.Regexp
var smtpClientLock *sync.RWMutex = &sync.RWMutex{}
var smtp *email.SMTPClient

func init() {
	// 初始化邮箱正则
	pattern := `^[^\s@]+@[^\s@]+\.[^\s@]+$`
	mailRe = regexp.MustCompile(pattern)
	commands := []*plugin.Command{}
	commands = append(commands, &plugin.Command{
		Prefix:         "/guard",
		Role:           constant.RoleOwner,
		DisablePrivate: true,
		Handle:         setGuardStatus,
	})

	commands = append(commands, &plugin.Command{
		Prefix:         "/guardstatus",
		Role:           constant.RoleMember,
		DisablePrivate: true,
		Handle:         getGuardStatus,
	})

	commands = append(commands, &plugin.Command{
		Prefix:         "/grouprule",
		Role:           constant.RoleOwner,
		DisablePrivate: true,
		Handle:         setGroupRule,
	})

	this := &plugin.Plugin{
		Id:              "GroupGuard",
		Name:            "入群验证",
		Commands:        commands,
		JoinGroupHandle: handle,
		ApplyConfig: func(m map[string]any) error {
			// 获取锁
			smtpClientLock.Lock()
			defer smtpClientLock.Unlock()

			server, _ := m["server"].(string)
			port, _ := m["port"].(int)
			encrypto, _ := m["encrypto"].(bool)
			account, _ := m["account"].(string)
			pwd, _ := m["pwd"].(string)
			smtp = &email.SMTPClient{
				Host:     server,
				Port:     port,
				UseSSL:   encrypto,
				Username: account,
				Password: pwd,
			}
			return nil
		},
		Config: []plugin.ConfigField{
			{Key: "server", Label: "SMTP服务器主机", Description: "SMTP服务器主机地址", Type: plugin.ConfigFieldTypeText},
			{Key: "port", Label: "SMTP服务器主机端口", Description: "SMTP服务器主机端口", Type: plugin.ConfigFieldTypeInt},
			{Key: "encrypto", Label: "SSL/TLS", Description: "是否启用加密", Type: plugin.ConfigFieldTypeBoolean},
			{Key: "account", Label: "账号", Description: "SMTP登录账号", Type: plugin.ConfigFieldTypeText},
			{Key: "pwd", Label: "SMTP密码", Description: "SMTP登录密码", Type: plugin.ConfigFieldTypePassword},
		},
	}

	plugin.Register(this)
}

func setGroupRule(ctx *context.MessageContext) error {
	if ctx.Content == "" {
		return ctx.Text("请输入群规URL").Send()
	}
	ctx.GroupStorage.Set("group_rule", ctx.Content)
	ctx.Text("已设置群规URL").Send()
	return nil
}

func setGuardStatus(ctx *context.MessageContext) error {
	var groupEnable bool
	if ok, err := ctx.GroupStorage.Get("enable_guard", &groupEnable); !ok || err != nil {
		ctx.GroupStorage.Set("enable_guard", false)
		groupEnable = false
	}
	groupEnable = !groupEnable
	ctx.GroupStorage.Set("enable_guard", groupEnable)
	if groupEnable {
		return ctx.Text("已启用入群验证").Send()
	} else {
		return ctx.Text("已关闭入群验证").Send()
	}
}

func getGuardStatus(ctx *context.MessageContext) error {
	var groupEnable bool
	if ok, err := ctx.GroupStorage.Get("enable_guard", &groupEnable); !ok || err != nil {
		ctx.GroupStorage.Set("enable_guard", false)
		groupEnable = false
	}
	if groupEnable {
		return ctx.Text("当前群已启用入群验证").Send()
	} else {
		return ctx.Text("当前群已关闭入群验证").Send()
	}
}

func handle(ctx *context.ApplyJoinGroupContext) error {
	var groupEnable bool
	if ok, err := ctx.GroupStorage.Get("enable_guard", &groupEnable); !ok || err != nil {
		ctx.GroupStorage.Set("enable_guard", false)
		groupEnable = false
	}
	if !groupEnable || smtp == nil || smtp.Host == "" {
		// 当前群未启用
		return nil
	}
	fmt.Printf("收到入群请求: %v\n\n", ctx.Answer)
	if !mailRe.MatchString(ctx.Answer) {
		// 邮箱不合法
		// fmt.Printf("邮箱不合法")
		md, _ := ctx.MarkdownTemplate("InvaildMailFormat", &templates.Args{"mail": ctx.Answer})
		md.SetInitiativeMessage()
		err := md.Send()
		if err != nil {
			return err
		}
		return ctx.Deny("请输入正确格式的邮箱")
	}
	err := ctx.Accept()
	if err != nil {
		return err
	}
	ctx.Ban(time.Minute * 43200)

	// 生成验证链接
	path, _, err := ctx.RegisterTemporaryRoute(
		context.TemporaryRouteOptions{
			TTL:         10 * time.Minute,
			OneTime:     true,
			ContentType: "text/html; charset=utf-8",
		},
		func(r *http.Request) (any, error) {
			ctx.Unban()
			md := ctx.Markdown(fmt.Sprintf("<qqbot-at-user id=\"%v\" /> 验证已通过, 祝您聊天愉快\n> Powered by [Yearnstudio](https://yearn.studio)", ctx.UserId))
			md.SetInitiativeMessage()
			md.Send()
			return templates.FillHTMLTemplate("FinishGroupGuard", templates.Args{})
		},
	)
	md := ctx.Markdown(fmt.Sprintf("<qqbot-at-user id=\"%v\" /> 您好, 欢迎加群, 请查看发送至您的邮箱的验证链接, 验证成功后自动解除禁言", ctx.UserId))
	md.SetInitiativeMessage()
	rule_url := ""
	if ok, err := ctx.GroupStorage.Get("group_rule", &rule_url); ok && err == nil {
		k := &buttons.Keyboard{}
		btn, _ := k.AppendButton("showGroupRule", "查看群规", "进群即代表您认可并遵守此规定", buttons.Blue, 0)
		btn.SetHref(rule_url)
		btn.SetPermission(buttons.AllUser)
		md.Keyboard(k)
	}
	err = md.Send()
	if err != nil {
		fmt.Printf("发送Markdown消息时候出错: %v", err)
		return err
	}
	// 取得锁
	smtpClientLock.RLock()
	defer smtpClientLock.RUnlock()
	// 发送邮件
	err = smtp.SendMail("入群验证", fmt.Sprintf("请点击以下链接进行验证:<br>https://offical.bot.yearnstudio.cn%s", path), email.ContentTypeHTML, []string{ctx.Answer})
	if err != nil {
		return err
	}
	return nil
}
