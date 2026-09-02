# 插件临时网页路由

插件可以在运行时创建一个临时的公开网页链接。框架固定接收
`/_plugin/{pluginID}/{token}`，因此无需在服务启动后修改 Gin 路由表。

```go
path, remove, err := ctx.RegisterTemporaryRoute(
	context.TemporaryRouteOptions{
		TTL:         30 * time.Minute,
		Method:      http.MethodGet, // 省略时默认为 GET
		OneTime:     true,
		ContentType: "text/html; charset=utf-8",
	},
	func(request *http.Request) (any, error) {
		// 校验并消费保存在插件存储中的邮箱验证记录。
		return "<h1>邮箱验证成功</h1>", nil
	},
)
if err != nil {
	return err
}
defer remove() // 可选：提前使链接失效；重复调用安全。

// 将 "https://bot.example.com" + path 写入验证邮件。
```

`path` 是相对路径，插件应按部署域名拼成邮件中的完整 URL。令牌由
`crypto/rand` 生成，长度为 32 字节。插件 ID 从当前 `Context` 自动取得，插件无需传递它。

处理函数收到原始 `*http.Request`。返回 `string` 或 `[]byte` 会直接作为内容，
其他值会编码为 JSON；返回 `nil` 会得到 `204 No Content`。如需状态码或响应头，返回
`context.HTTPResponse`：

```go
return context.HTTPResponse{
	Status: http.StatusCreated,
	Headers: http.Header{"X-Verification": {"accepted"}},
	Body: map[string]bool{"verified": true},
}, nil
```

路由超时、令牌不存在、插件 ID 不匹配及一次性链接已被消费时，均返回 `404`。
方法不匹配返回 `405` 并带 `Allow` 响应头。一次性链接在调用处理函数前即被原子地移除，
避免并发请求重复验证。

临时路由存储在进程内存中，服务重启后全部失效。需要恢复验证状态时，应把邮箱、群和
用户等业务数据保存在插件的 SQLite 存储中，并在用户重新请求邮件时创建新的链接。
