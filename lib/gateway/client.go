package gateway

import (
	"Plrx/lib/constant"
	"Plrx/lib/middleware"
	"Plrx/lib/qqapi"
	"Plrx/lib/structers"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Config 定义 WebSocket 网关客户端配置
type Config struct {
	Intents int    // 订阅的 intents 位掩码
	Shard   []int  // 分片配置 [shard_index, shard_count]
	Gateway string // 可选手动指定的网关 WSS 地址
}

// Client 管理与 QQ 机器人网关的 WebSocket 长连接
type Client struct {
	config    Config
	qqClient  *qqapi.Client
	conn      *websocket.Conn
	sessionID string
	latestSeq int64
	canResume bool

	writeMu   sync.Mutex
	stateMu   sync.RWMutex
	stopChan  chan struct{}
	stopped   atomic.Bool
	connDone  chan struct{}

	heartbeatInterval time.Duration
	lastAckReceived   time.Time
}

// NewClient 创建一个新的网关 WebSocket 客户端
func NewClient(config Config, qqClient *qqapi.Client) *Client {
	shard := config.Shard
	if len(shard) < 2 {
		shard = []int{0, 1}
	}
	config.Shard = shard

	// 若未指定 intents，默认订阅群聊、私聊与互动事件
	if config.Intents == 0 {
		config.Intents = constant.IntentGroupAndC2C | constant.IntentInteraction
	}

	return &Client{
		config:   config,
		qqClient: qqClient,
		stopChan: make(chan struct{}),
	}
}

// Start 启动网关连接并保持重连循环，直到 Stop 被调用
func (c *Client) Start() {
	log.Printf("[WebSocket] 网关客户端启动中, Intents: %d, Shard: %v", c.config.Intents, c.config.Shard)

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for !c.stopped.Load() {
		connectStart := time.Now()
		err := c.connect()
		if c.stopped.Load() {
			break
		}

		// 如果连接持续超过1分钟，说明连接是稳定的，重置退避时间
		if time.Since(connectStart) > 1*time.Minute {
			backoff = 1 * time.Second
		}

		if err != nil {
			log.Printf("[WebSocket] 连接异常断开: %v, 将在 %v 后尝试重连", err, backoff)
		} else {
			log.Printf("[WebSocket] 连接关闭, 将在 %v 后尝试重连", backoff)
		}

		select {
		case <-c.stopChan:
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Stop 停止网关客户端并断开连接
func (c *Client) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		close(c.stopChan)
		c.closeConn()
		log.Printf("[WebSocket] 网关客户端已停止")
	}
}

// 获取网关连接地址
func (c *Client) getGatewayURL() (string, error) {
	if c.config.Gateway != "" {
		return c.config.Gateway, nil
	}
	info, err := c.qqClient.GetGateway()
	if err != nil {
		return "", fmt.Errorf("调用 GetGateway 失败: %w", err)
	}
	if info.URL == "" {
		return "", errors.New("网关返回的 URL 为空")
	}
	return info.URL, nil
}

// connect 建立一次完整的 WebSocket 连接生命周期
func (c *Client) connect() error {
	wsURL, err := c.getGatewayURL()
	if err != nil {
		return err
	}

	log.Printf("[WebSocket] 正在连接网关: %s", wsURL)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	c.setConn(conn)
	defer c.closeConn()

	connDone := make(chan struct{})
	c.connDone = connDone

	// 等待并处理 OpCode 10 Hello 消息
	hello, err := c.waitHello()
	if err != nil {
		return fmt.Errorf("wait hello: %w", err)
	}

	interval := time.Duration(hello.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 45 * time.Second
	}
	c.heartbeatInterval = interval
	c.lastAckReceived = time.Now()
	log.Printf("[WebSocket] 收到 Hello, 心跳周期: %v", interval)

	// 进行鉴权 (Resume 或 Identify)
	c.stateMu.RLock()
	canResume := c.canResume && c.sessionID != ""
	c.stateMu.RUnlock()

	if canResume {
		log.Printf("[WebSocket] 尝试恢复会话 (Resume), SessionID: %s, Seq: %d", c.sessionID, atomic.LoadInt64(&c.latestSeq))
		if err := c.sendResume(); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
	} else {
		log.Printf("[WebSocket] 发送鉴权信息 (Identify)...")
		if err := c.sendIdentify(); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
	}

	// 启动心跳协程
	heartbeatDone := make(chan struct{})
	go c.heartbeatLoop(interval, heartbeatDone)
	defer close(heartbeatDone)

	// 进入消息读取循环
	return c.readLoop()
}

// setConn 安全设置当前连接
func (c *Client) setConn(conn *websocket.Conn) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn = conn
}

// closeConn 安全关闭连接
func (c *Client) closeConn() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// writeJSON 线程安全写入 JSON 数据
func (c *Client) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return errors.New("websocket connection is nil")
	}
	return c.conn.WriteJSON(v)
}

// waitHello 等待网关下发的第一条 OpCode 10 Hello 消息
func (c *Client) waitHello() (*HelloData, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, msgBytes, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	_ = c.conn.SetReadDeadline(time.Time{})

	var msg GatewayMessage
	if err := json.Unmarshal(msgBytes, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal hello message: %w", err)
	}

	if msg.Op != constant.OpCodeHello {
		return nil, fmt.Errorf("expected OpCode 10 Hello, got OpCode %d", msg.Op)
	}

	var hello HelloData
	if err := json.Unmarshal(msg.D, &hello); err != nil {
		return nil, fmt.Errorf("unmarshal hello data: %w", err)
	}

	return &hello, nil
}

// sendIdentify 发送 OpCode 2 Identify 消息
func (c *Client) sendIdentify() error {
	token, err := c.qqClient.GetAccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	data := IdentifyData{
		Token:   fmt.Sprintf("QQBot %s", token),
		Intents: c.config.Intents,
		Shard:   [2]int{c.config.Shard[0], c.config.Shard[1]},
		Properties: map[string]string{
			"$os":      runtime.GOOS,
			"$browser": "Plrx",
			"$device":  "Plrx",
		},
	}

	msg := struct {
		Op int          `json:"op"`
		D  IdentifyData `json:"d"`
	}{
		Op: constant.OpCodeIdentify,
		D:  data,
	}

	return c.writeJSON(msg)
}

// sendResume 发送 OpCode 6 Resume 消息
func (c *Client) sendResume() error {
	token, err := c.qqClient.GetAccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	c.stateMu.RLock()
	sessionID := c.sessionID
	seq := atomic.LoadInt64(&c.latestSeq)
	c.stateMu.RUnlock()

	data := ResumeData{
		Token:     fmt.Sprintf("QQBot %s", token),
		SessionID: sessionID,
		Seq:       seq,
	}

	msg := struct {
		Op int        `json:"op"`
		D  ResumeData `json:"d"`
	}{
		Op: constant.OpCodeResume,
		D:  data,
	}

	return c.writeJSON(msg)
}

// sendHeartbeat 发送 OpCode 1 心跳
func (c *Client) sendHeartbeat() error {
	seq := atomic.LoadInt64(&c.latestSeq)
	var d *int64
	if seq > 0 {
		d = &seq
	}

	msg := HeartbeatMessage{
		Op: constant.OpCodeHeartbeat,
		D:  d,
	}
	return c.writeJSON(msg)
}

// heartbeatLoop 周期性发送心跳
func (c *Client) heartbeatLoop(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			// 检查上次心跳响应是否超时
			if time.Since(c.lastAckReceived) > 3*interval {
				log.Printf("[WebSocket] 心跳超时未收到 ACK, 触发主动断开重连")
				c.closeConn()
				return
			}

			if err := c.sendHeartbeat(); err != nil {
				log.Printf("[WebSocket] 发送心跳失败: %v", err)
				c.closeConn()
				return
			}
		}
	}
}

// readLoop 读取网关消息并处理
func (c *Client) readLoop() error {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.handleDisconnect(err)
			return err
		}

		var msg GatewayMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[WebSocket] 解析网关消息失败: %v", err)
			continue
		}

		if msg.S > 0 {
			atomic.StoreInt64(&c.latestSeq, msg.S)
		}

		switch msg.Op {
		case constant.OpCodeDispatch:
			c.handleDispatch(msg.T, msg.D, raw)
		case constant.OpCodeHeartbeat:
			// 服务端请求心跳，立即回复
			_ = c.sendHeartbeat()
		case constant.OpCodeHeartbeatACK:
			c.lastAckReceived = time.Now()
		case constant.OpCodeReconnect:
			log.Printf("[WebSocket] 收到服务端 Reconnect 指令 (OpCode 7), 准备重新连接并恢复会话")
			c.stateMu.Lock()
			c.canResume = true
			c.stateMu.Unlock()
			c.closeConn()
			return errors.New("server requested reconnect")
		case constant.OpCodeInvalidSession:
			log.Printf("[WebSocket] 会话无效 (OpCode 9), 将清除 Session 并重新鉴权")
			c.stateMu.Lock()
			c.sessionID = ""
			atomic.StoreInt64(&c.latestSeq, 0)
			c.canResume = false
			c.stateMu.Unlock()
			c.closeConn()
			return errors.New("invalid session")
		default:
			log.Printf("[WebSocket] 未知 OpCode: %d", msg.Op)
		}
	}
}

// handleDispatch 处理 OpCode 0 业务事件
func (c *Client) handleDispatch(t string, d json.RawMessage, raw []byte) {
	switch t {
	case "READY":
		var ready ReadyData
		if err := json.Unmarshal(d, &ready); err != nil {
			log.Printf("[WebSocket] 解析 READY 事件失败: %v", err)
			return
		}
		c.stateMu.Lock()
		c.sessionID = ready.SessionID
		c.canResume = true
		c.stateMu.Unlock()
		log.Printf("[WebSocket] 机器人就绪 (READY), 用户: %s (ID: %s), SessionID: %s",
			ready.User.Username, ready.User.ID, ready.SessionID)
	case "RESUMED":
		c.stateMu.Lock()
		c.canResume = true
		c.stateMu.Unlock()
		log.Printf("[WebSocket] 会话恢复完成 (RESUMED)")
	default:
		// 校验并分发给插件处理器
		if !constant.IsValidEventType(t) {
			return
		}
		var payload structers.Payload
		if err := json.Unmarshal(raw, &payload); err != nil {
			log.Printf("[WebSocket] 解析事件 Payload 失败: %v", err)
			return
		}
		payload.EventType = constant.EventType(t)
		go middleware.ProcessPayload(payload, c.qqClient)
	}
}

// handleDisconnect 处理连接断开与错误码
func (c *Client) handleDisconnect(err error) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		log.Printf("[WebSocket] 连接关闭: code=%d, text=%s", closeErr.Code, closeErr.Text)
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		switch closeErr.Code {
		case 4009, 4008:
			// 连接过期或发送过快，可 Resume 重连
			c.canResume = true
		case 4006, 4007:
			// 无效 session id 或 seq 错误，无法 resume，必须重新 identify
			c.sessionID = ""
			atomic.StoreInt64(&c.latestSeq, 0)
			c.canResume = false
		case 4001, 4002, 4010, 4011, 4012, 4013, 4014, 4914, 4915:
			// 致命配置错误或封禁/下架
			log.Printf("[WebSocket] 严重错误 (code=%d): 请检查机器人状态、权限或配置", closeErr.Code)
			c.sessionID = ""
			atomic.StoreInt64(&c.latestSeq, 0)
			c.canResume = false
		default:
			// 其它断开（包括正常关闭、网络异常等），如有 sessionID 则尝试 resume
			if c.sessionID != "" {
				c.canResume = true
			}
		}
	} else {
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		if c.sessionID != "" {
			c.canResume = true
		}
	}
}
