package group

import (
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
	// 取得锁
	smtpClientLock.RLock()
	defer smtpClientLock.RUnlock()

	// 生成验证链接
	path, _, err := ctx.RegisterTemporaryRoute(
		context.TemporaryRouteOptions{
			TTL:     10 * time.Minute,
			OneTime: true,
		},
		func(r *http.Request) (any, error) {
			ctx.Unban()
			return "验证成功", nil
		},
	)

	// 发送邮件
	err = smtp.SendMail("入群验证", fmt.Sprintf("请点击以下链接进行验证:<br>https://offical.bot.yearnstudio.cn/%s", path), email.ContentTypeHTML, []string{ctx.Answer})
	if err != nil {
		return err
	}
	return nil
}
