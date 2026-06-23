package ping

import (
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/parser"
	"Plrx/lib/plugin"
	"Plrx/lib/structers"
	"fmt"
	"net"
	"os"
	"reflect"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type PingData struct {
	Ip string
}

func init() {
	var commands []*plugin.Command
	commands = append(commands, &plugin.Command{
		Prefix:       "/ping",
		Role:         constant.RoleAdmin,
		Describle:    "发起ping请求",
		Handle:       Ping,
		Parser:       &parser.PositionalParser{},
		ParserTarget: reflect.TypeOf(PingData{}),
	})
	self := plugin.PluginConfig{
		Id:       "ping",
		Commands: commands,
	}
	plugin.Register(&self)
}

func Ping(ctx *context.Context) error {
	args, ok := ctx.Parserd.(*PingData)
	if !ok || args.Ip == "" {
		return fmt.Errorf("Error: invalid IP address")
	}

	// 尝试解析目标 IP
	dstAddr, err := net.ResolveIPAddr("ip4", args.Ip)
	if err != nil {
		ctx.Reply(fmt.Sprintf("### 解析结果\n```\n目标 %v 解析失败\n```", args.Ip), structers.Markdown)
		return fmt.Errorf("解析IP失败: %v", err)
	}

	// 监听 ICMP 协议
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("创建ICMP监听失败 (需Root权限): %v", err)
	}
	defer conn.Close()

	// 构建 ICMP Echo 请求
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("PING"),
		},
	}

	binaryMsg, _ := msg.Marshal(nil)

	// 发送
	start := time.Now()
	_, err = conn.WriteTo(binaryMsg, dstAddr)
	if err != nil {
		return fmt.Errorf("发送失败: %v", err)
	}

	// 等待响应
	reply := make([]byte, 1500)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		ctx.Reply(fmt.Sprintf("### 响应结果\n```\n目标 %v 超时或不可达\n```", args.Ip), structers.Markdown)
		return fmt.Errorf("目标 %s 超时或不可达", args.Ip)
	}

	duration := time.Since(start)

	// 解析响应
	parsedMsg, err := icmp.ParseMessage(1, reply[:n])
	if err != nil {
		return fmt.Errorf("解析响应失败")
	}

	switch parsedMsg.Body.(type) {
	case *icmp.Echo:
		// 成功
		fmt.Printf("成功: 目标 %s 响应, 耗时: %v\n", args.Ip, duration)
		ctx.Reply(fmt.Sprintf("### 响应结果\n```\n来自 %v 的回复, 时间 = %v\n```", args.Ip, duration), structers.Markdown)
		return nil
	default:
		ctx.Reply("### 响应结果\n```\n收到非echo响应\n```", structers.Markdown)
		return fmt.Errorf("收到非Echo响应")
	}
}
