package code40

import (
	"botOffical/lib/constant"
	"botOffical/lib/context"
	"botOffical/lib/parser"
	"botOffical/lib/plugin"
	"botOffical/lib/structers"
	"botOffical/lib/templates"
	"fmt"
	"reflect"
)

type QueryArgs struct {
	Target string
	Id     int
}

type UserAPIData struct {
	Code int           `json:"code"`
	Data []UserProfile `json:"data"`
}

type WorkAPIData struct {
	Code int      `json:"code"`
	Data WorkInfo `json:"data"`
}

type WorkInfo struct {
	Id          int    `json:"id"`
	OpenSource  int    `json:"opensource"`
	Publish     int    `json:"publish"`
	Author      int    `json:"author"`
	Nickname    string `json:"nickname"`
	Introduce   string `json:"introduce"`
	Name        string `json:"name"`
	Image       string `json:"image"`
	Look        int    `json:"look"`
	Like        int    `json:"like"`
	Collections int    `json:"num_collections"`
}

type UserProfile struct {
	Id        int    `json:"id"`
	Nickname  string `json:"nickname"`
	Head      string `json:"head"`
	Fan       int    `json:"fan"`
	Follow    int    `json:"follow"`
	Coins     int    `json:"coins"`
	Introduce string `json:"introduce"`
}

func init() {
	var commands []*plugin.Command
	commands = append(commands, &plugin.Command{
		Prefix:    "/40code",
		Role:      constant.RoleMember,
		Describle: "查询40code相关信息",
		Handle: func(ctx *context.Context) error {
			args, ok := ctx.Parserd.(*QueryArgs)
			if !ok {
				ctx.Reply("参数错误", structers.PlainText)
				return fmt.Errorf("参数错误")
			}
			switch args.Target {
			case "user":
				var data UserAPIData
				err := ctx.Requests.Get(fmt.Sprintf("https://api.abc.520gxx.com/user/info?id=%v", args.Id), &data, make(map[string]string))
				if err != nil {
					ctx.Reply(fmt.Sprintf("## 40code API请求异常\n```\n%v\n```", err), structers.Markdown)
					return err
				}
				if len(data.Data) < 1 {
					ctx.Reply("## 40code API请求异常\n```\n没有这个用户的数据\n```", structers.Markdown)
					return fmt.Errorf("不存在id为%v的40code用户", args.Id)
				}
				ctx.Reply(fmt.Sprintf("## %v\n粉丝:**%v** | 关注:**%v** | 金币:**%v**\n![用户头像 #200px #200px](%v)", data.Data[0].Nickname, data.Data[0].Fan, data.Data[0].Follow, data.Data[0].Coins, fmt.Sprintf("https://abc.520gxx.com/static/internalapi/asset/%v", data.Data[0].Head)), structers.Markdown)
				ctx.Reply(data.Data[0].Introduce, structers.Markdown)
				return nil
			case "work":
				var data WorkAPIData
				err := ctx.Requests.Get(fmt.Sprintf("https://api.abc.520gxx.com/work/info?id=%v", args.Id), &data, map[string]string{})
				if err != nil {
					ctx.Reply(fmt.Sprintf("## 40code API请求异常\n```\n%v\n```", err), structers.Markdown)
					return err
				}
				tmp, err := templates.FillMarkdownTemplate("40codeWorkInfo", templates.Args{
					"name":        data.Data.Name,
					"look":        data.Data.Look,
					"like":        data.Data.Like,
					"collections": data.Data.Collections,
					"nickname":    data.Data.Nickname,
					"author":      data.Data.Author,
					"image":       fmt.Sprintf("https://abc.520gxx.com/static/internalapi/asset/%v", data.Data.Image),
				})
				if err != nil {
					ctx.Reply(fmt.Sprintf("Markdown填充异常: %v", err), structers.PlainText)
				}
				data.Data.Introduce = templates.ProcessMarkdownImages(data.Data.Introduce)
				ctx.Reply(tmp, structers.Markdown)
				ctx.Reply(data.Data.Introduce, structers.Markdown)
			}
			return nil
		},
		Parser:       &parser.PositionalParser{},
		ParserTarget: reflect.TypeOf(QueryArgs{}),
	})

	self := plugin.PluginConfig{
		Id:       "40code",
		Commands: commands,
	}
	plugin.Register(&self)
}
