## QQ官方机器人 轻量开发框架

### 配置文件
在根目录下新建`config.json`, 并按照下方格式填写
```json
{
  "port": 8080,
  "appid": "10000",
  "secret": "11111",
  "proxy": "https://api.sgroup.qq.com"
}
```

#### 配置文件说明
- port    服务端口
- appid   机器人ID
- secret  机器人AppSecret
- proxy   代理地址

> 什么是代理地址?
> 
> 代理地址是为了QQ开放平台IP白名单限制所使用的功能, 当你的服务器处于动态IP的时候, 可以在一个固定IP的设备上搭建反代服务, 然后填写对应的地址

### WebHook配置
在QQ开放平台里配置, WebHook填写`你的地址:端口/webhook`

事件按照你的需求勾选, 也可以一次性全部选择

### 组织方式
框架使用`插件`的形式来增加功能

#### 新建插件
在`plugins`(注意不是`lib/plugin`)目录下新建一个文件夹, 然后放入你的插件代码

如下是一个插件模板
```go
package echo

import (
	"botOffical/lib/constant"
	"botOffical/lib/context"
	"botOffical/lib/plugin"
	"botOffical/lib/structers"
)

func init() {
	var commands []*plugin.Command
	commands = append(commands, &plugin.Command{
		Prefix:    "/echo",
		Role:      constant.RoleMember,
		Describle: "回显",
		Handle:    echoHandle,
	})

	self := plugin.PluginConfig{
		Id:       "echo",
		Commands: commands,
	}
	plugin.Register(&self)
}

func echoHandle(ctx *context.Context) error {
	return ctx.Client.SendGroupMessage(*ctx.Message, ctx.Message.GroupId)
}
```

#### 插件元信息

- Id          插件ID, 用于日志排查
- Commands    指令列表, 用于注册指令

#### 新建指令
一个指令需要`前缀` / `使用权限` / `描述`(暂无功能) / `处理函数`

并且可以额外添加`解析器`及`解析模板`

##### 前缀
> Prefix
指令前缀, 只有以该前缀开头的指令会传入插件

根据注册顺序, 后注册的插件如果根之前注册插件的前缀相同, 会发生**覆盖**

##### 使用权限
> Role | 枚举值: **constant.RoleMember** | **constant.RoleAdmin** | **constant.RoleOwner**
最低使用指令的成员身份, 依次为**普通成员**、**管理员**和**群主**

不满足身份要求会静默失败

##### 处理函数
> Handle | type HandleFunc func(*context.Context) error
其中`*context.Context`为上下文对象, 其API用法见后文

函数需要返回一个`error`, 会显示在日志里, 不会发送到QQ里

##### 解析器&解析模板
> Parser & ParserTarget
> 
> 两者必须合用, 否则可能引发panic或预期之外的行为
解析器接受一个`Parser`接口, 其需要一个`Parse(rawMsg string, result any) error`函数, 该函数接收**原始消息**及**接收者指针**并返回一个`error`

- 当`Parser`没有被指定时, 默认使用`DefaultParser`(lib/parser/default.go), 除此之外还提供一个`PositionalParser`解析器

`DefaultParser`会将**原始消息**直接传给**接收者**, 不做任何处理

`PositionalParser`必须和`ParserTarget`配合使用, 会将指令参数解析到结构体里

- 当`ParserTarget`没有指定时, 默认使用`string`类型

当解析器为`PositionalParser`, 必须指定`ParserTarget`为一个从**结构体**构造的`reflect.Type`对象(`reflect.TypeOf`), 可以参考**ping**插件


#### 注册插件

在`plugins/register.go`中**匿名导入**你的插件所在的包

```go
import	_ "botOffical/plugins/ping"

```

### 上下文对象

这里假设传入的Context被`ctx`变量接收

#### 发送消息
有两种方式, 一种是手动构造`Message`对象, 而更推荐的是调用`Reply`快捷函数

##### Reply函数

直接调用传入的`ctx`的`Reply`函数:
```go
ctx.Reply("", structers.PlainText)
```
第一个参数是**消息内容**, 第二个参数是**消息类型**

**消息类型**为`MessageType`枚举, 可以选择`PlainText`及`Markdown`两种类型, 第一个为`纯文本`, 第二个为`Markdown`

**消息内容**在消息类型为`Markdown`的时候, 会被渲染为Markdown

##### Message对象
> 该对象的定义位于**lib/structers/msg.go**

1. 构造Message对象
你至少需要定义如下内容:
- Content
- MessageType
参数的含义与Reply中的一致

2. 发送消息
调用`ctx.Client.SendGroupMessage`或`ctx.Client.SendPrivateMessage`(根据发送目标)

当调用`ctx.Client.SendGroupMessage`时, 默认为**主动推送**消息, 如果需要采取**被动回复**(这两者区别见QQ官方文档), 需要在`Message`结构体中填入`MessageId`参数, 指定回复消息的ID

两个函数的**第一个参数**均为`Message`对象, 第二个参数分别为**群OpenID**及**用户ID**, 其中群OpenID的查询可以使用`echo`插件下的`/groupid`指令

#### 消息内容

##### 原始消息
位于`ctx.Message.Content`

##### 解析器产物
位于`ctx.Parserd`, 必须进行**类型断言**

##### 消息ID
位于`ctx.Message.MessageId`

##### 消息对象
位于`ctx.Message`

#### 发送者信息

包含在`ctx.Message`中, 为其下的`UserId`、`UnionId`及`GroupId`字段

#### 消息来源

目前仅存在两种枚举: `PrivateMessage`及`GroupMessage`

## 许可证

MIT