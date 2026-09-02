# Polarix 北极星

> QQ官方机器人 轻量开发框架
>
> Without AI feature

## 快速开始

1. 复制 `config.example.json` 为 `config.json`，填写 AppID / AppSecret / admin_password
2. `go build .` 或 `make check`（质量门：fmt + vet + test + build）
3. 启动后访问 `/admin` 管理插件；图床到 `/admin/assets` 配置

### 配置文件

根目录已有 `config.example.json`：完整字段示例（全部事件、插件、权限示例）。直接复制成 `config.json` 按需填写。

```json
{
  "port": 8080,
  "appid": "10000",
  "secret": "11111",
  "proxy": "https://api.sgroup.qq.com",
  "protocol": "webhook",
  "intents": ["GROUP_AT_MESSAGE_CREATE", "INTERACTION_CREATE"],
  "global_markdown": false,
  "markdown_verify_image": false,
  "retry_when": [11253, 630006],
  "upload_threshold": 3145728
}
```

图床运行配置不在 `config.json` 里，独立存放于本地 `assets.json`（见「图床（assets）」）。

#### 可订阅事件（intents）

| 事件 | 说明 |
|---|---|
| `GROUP_AT_MESSAGE_CREATE` | 群里 @ 机器人消息 |
| `GROUP_MESSAGE_CREATE` | 群内全部消息 |
| `C2C_MESSAGE_CREATE` | 私聊消息 |
| `INTERACTION_CREATE` | 互动事件（按钮回调等） |
| `GROUP_JOIN_REQUEST` | 入群申请 |
| `GROUP_MEMBER_ADD` | 新成员入群 |
| `GROUP_MEMBER_REMOVE` | 成员退群 |
| `MESSAGE_AUDIT_PASS` | 消息审核通过 |
| `MESSAGE_AUDIT_REJECT` | 消息审核驳回 |
| `GROUP_ADD_ROBOT` | 机器人被拉入群 |
| `GROUP_DEL_ROBOT` | 机器人被移出群 |

#### 配置文件说明

- port              服务端口
- appid             机器人ID
- secret            机器人AppSecret
- proxy             代理地址
- protocol          接入协议: `webhook`(默认, 平台推送回调) 或 `websocket`(长连接网关)
- intents           websocket 模式下订阅的事件名列表
- global_markdown   全局 markdown 模式: 所有文字消息统一按 markdown 渲染, 图片/按钮内联
- markdown_verify_image  markdown 图片转存失败时中断发送
- retry_when        发送遇到这些 QQ 业务错误码时自动重试
- upload_threshold  超过该字节数的文件走分片上传(默认 3MB)
- admin_password    管理面板密码；不设置时仅本机可访问
- 流式消息 API: `SendStreamMessage` / `StreamSession` 支持 markdown 分片发送到私聊（`POST /v2/users/{id}/stream_messages`）

> 什么是代理地址?
>
> 代理地址是为了QQ开放平台IP白名单限制所使用的功能, 当你的服务器处于动态IP的时候, 可以在一个固定IP的设备上搭建反代服务, 然后填写对应的地址

### 接入配置

**Webhook 模式**（默认）：QQ 开放平台里配置回调地址为 `你的地址:端口/webhook`，事件按需勾选。

**WebSocket 模式**：`"protocol": "websocket"`，框架自动连接网关长连接，无需公网回调地址。管理面板（`/admin`）与主动推送接口（`/push`）仍可用。

### 图床（assets）

框架以注册表驱动图床：启动时扫描当前环境已导入的 provider 实现，管理面板 `/admin/assets` 自动列出全部 provider 及其配置字段，保存即写入本地 `assets.json`（不入 git）并热更新。

- 每个 provider 有 `enabled`（启用开关）与 `priority`（数值越大越优先，同级保持配置顺序）
- 上传时按优先级从高到低依次尝试，失败自动切换下一个，全失败时图片保持原样
- `whitelist` 命中的图片 URL 跳过图床，原样透传
- 管理面板中密钥字段（password）不回显：留空保存表示不修改原密钥

**assets.json 格式（示意，具体字段以面板显示为准）：**
```json
{
  "whitelist": ["https://q.qlogo.cn"],
  "providers": [
    { "name": "example", "enabled": true, "priority": 100, "config": { "token": "xxx" } }
  ]
}
```

#### 自己实现一个 provider

框架用显式注册驱动，新 provider 只需实现 `assets.ImageProvider`（`Name()` + `Upload(ctx, input) (url, error)`）并在 `init()` 中注册；注册后管理面板自动出现配置表单，无需改任何页面代码。

```go
// lib/assets/providers/mine.go
package providers

import (
	"Plrx/lib/assets"
	"context"
	"fmt"
)

func init() {
	assets.Register("mine", newMine, []assets.ConfigField{
		{Key: "token", Label: "访问令牌", Type: "password", Required: true},
		{Key: "endpoint", Label: "上传地址", Type: "text", Default: "https://api.example.com/upload"},
	})
}

type mine struct {
	cl    *assets.Client
	token string
}

func newMine(cl *assets.Client, cfg map[string]any) (assets.ImageProvider, error) {
	token, _ := cfg["token"].(string)
	if token == "" {
		return nil, fmt.Errorf("mine: token 必填")
	}
	return &mine{cl: cl, token: token}, nil
}

func (p *mine) Name() string { return "mine" }

func (p *mine) Upload(ctx context.Context, in assets.ProviderInput) (string, error) {
	// multipart 上传，resp 按实际接口定义
	mp := assets.NewMultipart()
	mp.AddFile("file", in.Filename, in.MimeType, in.Buffer)
	mp.Close()
	var resp struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := p.cl.PostMultipart("https://api.example.com/upload", mp, &resp, map[string]string{
		"Authorization": p.token,
	}); err != nil {
		return "", err
	}
	if resp.Data.URL == "" {
		return "", fmt.Errorf("mine: 响应缺少 url")
	}
	return resp.Data.URL, nil
}
```

> 提示：`config.example.json` 中的 `assets` 段只是给你复制到本地 `assets.json` 用的格式参考，不包含任何具体 provider 实现细节。所有配置（含密钥）只存在于你自己的机器上。

***

### 组织方式

框架使用`插件`的形式来增加功能

### 插件管理面板

启动服务后访问 `/admin` 查看插件目录，每个插件进入独立的 `/admin/plugins/{id}` 配置页面。页面支持跟随系统、浅色和深色三种主题，选择会保存在浏览器中。插件配置保存到 `config.json` 的 `plugin_settings` 字段并即时生效。生图插件可在此配置 OpenAI 兼容接口，用户通过 `/draw <图片描述>` 生成图片；发送指令时附带一张或多张图片会自动调用 `/images/edits`，将附件作为参考图，最多使用 16 张。

未设置 `admin_password` 时，管理页面仅允许从服务器本机访问。需要远程管理时，在 `config.json` 中增加管理密码：

```json
{
  "admin_password": "请设置高强度密码"
}
```

远程访问时使用 HTTP Basic Auth，用户名固定为 `admin`，密码为 `admin_password`。生产环境应通过 HTTPS 反向代理访问管理页面。

插件通过 `Plugin.Config` 声明配置项，不需要自行实现管理页面或 HTTP 接口。`password` 字段只向面板返回是否已配置，留空保存时会保留原值：

```go
plugin.Register(&plugin.Plugin{
    Id:          "example",
    Name:        "示例插件",
    Description: "插件配置示例",
    Config: []plugin.ConfigField{
        {Key: "enabled", Label: "启用插件", Type: "boolean"},
        {Key: "endpoint", Label: "接口地址", Type: "text"},
        {Key: "api_key", Label: "API Key", Type: "password"},
    },
    ValidateConfig: validateConfig,
    ApplyConfig:    applyConfig,
})
```

`ValidateConfig` 在写入配置前执行，`ApplyConfig` 在启动加载和面板保存后执行。

指令还可以声明生命周期钩子：

```go
&plugin.Command{
    Prefix: "/example",
    Handle: handle,
    PermissionDenied: func(ctx *context.MessageContext) error {
        return ctx.Text("你无权使用此指令").Send()
    },
    HandleError: func(ctx *context.MessageContext, commandErr error) error {
        return ctx.Text("指令执行失败").Send()
    },
}
```

`PermissionDenied` 会在角色权限、私聊限制或黑白名单拒绝时调用。`HandleError` 会在 `Handle` 返回非空错误或发生 panic 后调用；原始错误及错误处理函数自身的错误仍会写入日志。子指令使用实际命中的子指令钩子。

管理面板也会为所有插件自动提供访问控制，无需插件额外声明。可以设置插件默认规则，并为具体指令或子指令覆盖：

- `关闭限制`：所有用户和群均可使用。
- `白名单`：用户 OpenID 或群 OpenID 命中任一名单时允许使用。
- `黑名单`：用户 OpenID 或群 OpenID 命中任一名单时拒绝使用。
- 指令选择 `继承插件规则` 时使用插件默认规则。

访问控制保存在 `config.json` 的 `plugin_access` 字段。群聊优先使用发送者的 `member_openid`，缺失时使用 `union_openid`；私聊使用 `user_openid`。被拒绝的指令会静默忽略。

#### 新建插件

在 `plugins`(注意不是 `lib/plugin`)目录下新建一个文件夹, 然后放入你的插件代码

如下是一个插件模板（参考 `plugins/echo/echo.go`）：

```go
package echo

import (
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/plugin"
)

func init() {
	plugin.Register(&plugin.Plugin{
		Id: "echo",
		Commands: []*plugin.Command{
			{
				Prefix:   "/echo",
				Role:     constant.RoleMember,
				Describe: "回显",
				Handle:   echoHandle,
			},
		},
	})
}

func echoHandle(ctx *context.MessageContext) error {
	return ctx.Text(ctx.Raw).Send()
}
```

#### 插件元信息

- Id          插件ID, 用于日志排查
- Commands    指令列表, 用于注册指令
- Config      配置项声明, 面板据此渲染表单
- ValidateConfig / ApplyConfig  配置校验与应用钩子

#### 新建指令

一个指令需要`前缀` / `使用权限` / `描述` / `处理函数`，并且可以额外添加`解析器`及`解析模板`、子指令、生命周期钩子

##### 前缀

> Prefix

指令前缀, 只有以该前缀开头的指令会传入插件。根据注册顺序, 后注册的插件如果跟之前注册插件的前缀相同, 会发生**覆盖**

##### 使用权限

> Role | 枚举值: **constant.RoleMember** | **constant.RoleAdmin** | **constant.RoleOwner**

最低使用指令的成员身份, 依次为**普通成员**、**管理员**和**群主**。不满足身份要求会静默失败

##### 处理函数

> Handle | type CommandHandleFunc func(*context.MessageContext) error

其中`*context.MessageContext`为上下文对象, 其API用法见后文。函数需要返回一个`error`, 会显示在日志里, 不会发送到QQ里

##### 子指令

> SubCommand []*Command

在指令下挂多个子指令, 匹配子指令前缀后进入子指令处理；未命中任何子指令时可用 `SubCommandFallback` 兜底

##### 解析器&解析模板

> Parser & ParserTarget

> 两者必须合用, 否则可能引发panic或预期之外的行为

解析器接受一个`Parser`接口, 其需要一个`Parse(rawMsg string, result any) error`函数, 该函数接收**原始消息**及**接收者指针**并返回一个`error`

- 当`Parser`没有被指定时, 默认使用`DefaultParser`(lib/parser/default.go), 除此之外还提供一个`PositionalParser`解析器
- `DefaultParser`会将**原始消息**直接传给**接收者**, 不做任何处理
- `PositionalParser`必须和`ParserTarget`配合使用, 会将指令参数解析到结构体里
- 当`ParserTarget`没有指定时, 默认使用`string`类型
- 当解析器为`PositionalParser`, 必须指定`ParserTarget`为一个从**结构体**构造的`reflect.Type`对象(`reflect.TypeOf`)

#### 注册插件

在`plugins/register.go`中**匿名导入**你的插件所在的包

```go
import _ "Plrx/plugins/echo"
```

***

### 上下文对象

这里假设传入的 `*context.MessageContext` 被 `ctx` 变量接收。`MessageContext` 内嵌 `*Context`（请求客户端、存储命名空间）与 `message.UserMessage`（`Content`、`Attachments`）。

#### 发送消息

推荐使用链式 API：`ctx.Text(...)` / `ctx.Markdown(...)` / `ctx.MarkdownTemplate(...)` / `ctx.Media(...)` 构造消息，最后 `.Send()` 自动按来源（群聊/私聊）发送：

```go
// 纯文本（全局 markdown 开启时自动转 markdown）
ctx.Text("你好").Send()

// Markdown（图片自动内嵌图床 URL + 尺寸标注）
ctx.Markdown("## 标题\n正文").Send()

// 填充 Markdown 模板
ctx.MarkdownTemplate("UserIdCard", &templates.Args{
    "id":     ctx.UserId,
    "msg_id": ctx.MessageId,
}).Send()

// 附带键盘按钮
msg := ctx.Markdown("请选择")
k := &buttons.Keyboard{}
btn, _ := k.AppendButton("1", "按钮", "已点击", buttons.Blue, 0)
btn.SetAutoCommand("/help", false, false)
msg.Keyboard(k)
msg.Send()
```

##### 主动推送

非回复场景的主动推送使用 `ctx.Client.SendGroupMessage(data, groupId)` / `ctx.Client.SendPrivateMessage(data, userId)`，其中 `data` 是消息序列化后的字节流（`[]byte`）。

#### 消息内容

- 原始消息: 位于`ctx.Raw`
- 消息内容: 位于`ctx.Content`
- 解析器产物: 位于`ctx.Parsed`, 必须进行**类型断言**
- 消息ID: 位于`ctx.MessageId`
- 事件ID: 位于`ctx.EventId`
- 发送者信息: `ctx.UserId`、`ctx.GroupId`
- 消息来源: `ctx.Target`（`constant.PrivateMessage` / `constant.GroupMessage`）

#### 入站解析增强

`MessageContext` 附带解析产物：

- `ctx.Mentions`  @提及列表
- `ctx.Quote`     引用消息
- `ctx.Emojis`     解码后的表情文本
- `ctx.AttachmentTypes`  附件分类（image/video/audio/file）
- `ctx.AvatarURL`  发送者头像

#### 存储 API

`ctx` 提供五个 SQLite 键值命名空间（五级作用域）：

- `ctx.GlobalStorage`   全局
- `ctx.PluginStorage`   当前插件
- `ctx.CommandStorage`  当前指令
- `ctx.UserStorage`     当前用户
- `ctx.GroupStorage`    当前群

每个 `*storage.Store` 提供 `Set(key, value)` / `Get(key, &target)` / `Has(key)` / `Delete(key)` / `Clear()`，value 为任意可 JSON 序列化的值：

```go
ctx.UserStorage.Set("coins", 42)
var coins int
if ok, _ := ctx.UserStorage.Get("coins", &coins); ok {
    ctx.Text(fmt.Sprintf("金币: %d", coins)).Send()
}
```

#### 公共请求对象

插件的请求应该调用上下文中的 `ctx.Request`, 该对象支持：

- `ctx.Request.Get(url string, result any, headers map[string]string) error`
- `ctx.Request.Post(url string, body any, result any, headers map[string]string) error`
- `ctx.Request.PostForm(url string, form url.Values, result any, headers map[string]string) error`
- `ctx.Request.PostMultipart(url string, mp *Multipart, result any, headers map[string]string) error`

- url 请求目标
- body 请求体(可以为`[]byte`或者为可以被`json.Marshal`的对象)
- result 返回结果绑定目标(需要包含json标签的结构体), nil时不解析
- headers 请求头

***

### Markdown模板

可以在`templates/markdown`下面存放多个`.md`文件, 每个文件为一个Markdown模板, 非`.md`文件会被忽略

在Markdown模板里, 可以使用插值语法:

```markdown
## {{ aaa }}
```

文件名(**不包含**.md后缀)将作为模板ID

通过调用`lib/templates`的`FillMarkdownTemplate(Id string, args Args)`函数可以填充模板，需要两个参数: `Id` 模板ID、`args` 参数列表

#### 参数列表

是由`type Args map[string]any`定义的, 可以通过:

```go
templates.Args{
	"name": data.Data.Name,
	"look": data.Data.Look,
}
```

的方式直接声明, 原本的`map[string]string`不再使用。参数支持 `string` / `int` / `int64` / `float64` / `bool`，以及**嵌套展开**的 `map[string]any` 与 `[]any`（列表）：

```go
templates.Args{
	"user":  map[string]any{"name": "张三"},
	"items": []any{"a", "b"},
}
```

占位符规则：`{{key}}` 标量、`{{key.#0}}` 列表第 0 项、`{{key.obj.prop}}` 嵌套对象字段。

#### 可能的错误

- 当模板ID不存在时, 返回错误
- 当参数列表args传入的参数不满足模板里定义的**所有**插值时, 返回错误
- 当参数列表args传入的结构体中有无法使用的类型时, 返回错误

#### 追加图片元信息

QQ的Markdown无法自适应图片大小, 必须追加元信息才能正常显示:

```markdown
![alt #300px #400px](https://aaa.com/bbb.jpg)
```

Markdown 消息发送前会自动处理图片（内嵌图床 URL + 尺寸标注，见 `lib/templates.ProcessMarkdownImages`），无需手动调用。

***

### 按钮

代码位于`lib/buttons/`, 示范在`echo`插件的`/echo`与`/random`指令

一个消息可以附带一个`Keyboard`, 一个`Keyboard`最多五行, 每行最多五个按钮, 共25个

#### 创建按钮

通过`&buttons.Keyboard{}`初始化一个变量(假设为`keyboard`), 作为承载按钮的变量，然后调用`keyboard.AppendButton`:

```go
button, err := keyboard.AppendButton("ID", "点击前文本", "点击后文本", buttons.Blue, 0)
```

- `"ID"` 按钮ID, 在一个Keyboard内必须唯一
- `"点击前文本"` & `"点击后文本"` 不予解释
- `buttons.Blue` 按钮边框样式（`lib/buttons` 下的枚举，只支持 `Gray` 和 `Blue`）
- `0` 在哪一行追加按钮, 从**0**开始, 最大为**4**

需要判断`err`是否为`nil`。`button`为`*Button`类型, 是修改按钮的指针, 不得进行值拷贝, 否则修改操作会失效

#### 设置按钮行为

调用`button`的函数

- `SetAutoCommand(content string, autoSend bool, anchor bool) *Button` 设置自动发送指令：参数为消息内容、是否自动发送(仅私聊有效)、是否拉起图片选择(仅手机端有效, 目前无法使用, 请保持`false`)
- `SetHref(url string) *Button` 设置跳转链接, 参数为链接地址, 需要携带协议头
- `SetCallback(data string, handle CallbackButtonHandleFunc) *Button` 设置回调及处理函数；仅注册回调标识时可使用 `SetCallbackWithoutHandle(data string)`

回调处理函数通过 `buttons.RegisterCallbackFunc(id, handle)` 注册，函数签名 `func(ctx *context.CallbackContext) error`，`ctx.Data` 为携带的回调数据。回调按钮由框架分发到对应插件上下文。

#### 设置按钮权限

调用`button.SetPermission(required buttons.AllowedPermission)`, 枚举位于 `lib/buttons`: `SomeUser` / `Admin`(仅管理员可用) / `AllUser`(所有人可用)

当需要设置部分用户可用时, 需要使用`button.SetUserWhiteList(users []string)`, 传入允许使用的用户的*OpenID*列表

#### 设置其他内容

##### 不支持按钮的情况

调用`button.SetUnsupportedTip(tip string)`设置不支持按钮的时候的提示文本

***

### 能力清单

- [x] 插件系统：注册 / 配置 / 权限 / 生命周期钩子
- [x] 管理面板：插件配置 + 访问控制 + 图床管理（/admin/assets）
- [x] 按钮功能与回调事件
- [x] 数据库 API（SQLite 键值，五级命名空间）
- [x] 图床聚合：注册表驱动 + 显式优先级 + 失败降级
- [x] 全局 Markdown + 图片内嵌 + 尺寸探测
- [x] 媒体上传（url 直传 / 分片上传 / 音视频文件）
- [x] WebSocket 网关 + Webhook 双协议
- [x] 入站消息解析（表情 / @ / 引用 / 附件 / 头像）
- [x] 错误码中文提示 + 业务码重试 + 消息审计等待
- [x] 流式消息（markdown 分片）
- [x] 主动推送接口（/push）

#### 不会支持的功能

- 所有与频道相关的功能

## 许可证

MIT
