package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/url"
	"time"

	"Plrx/lib/constant"
	"Plrx/lib/middleware"
	"Plrx/lib/qqapi"
	"Plrx/lib/structers"

	"github.com/gorilla/websocket"
)

const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
)

type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	T  string          `json:"t,omitempty"`
	S  int64           `json:"s,omitempty"`
	ID string          `json:"id,omitempty"`
}

// Client WebSocket 网关客户端。
type Client struct {
	api        *qqapi.Client
	gatewayURL string
	intents    int
	shard      [2]int
	conn       *websocket.Conn
	sessionID  string
	seq        int64
	ping       *time.Ticker
	acked      bool
	stop       chan struct{}
	stopped    chan struct{}
}

// New 创建网关客户端。
func New(api *qqapi.Client, gatewayURL string, intents int, shard [2]int) *Client {
	if shard[0] == 0 && shard[1] == 0 {
		shard = [2]int{0, 1}
	}
	return &Client{
		api:        api,
		gatewayURL: gatewayURL,
		intents:    intents,
		shard:      shard,
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
}

// Start 启动网关并阻塞直到连接断开（调用方应 go Start）。
func (c *Client) Start() {
	defer close(c.stopped)
	backoff := time.Second
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		if err := c.run(); err != nil {
			log.Printf("[gateway] 连接断开: %v，%.0fs 后重连", err, backoff.Seconds())
		}
		// jitter 抖动：随机 ±25% 偏移，避免多实例断线后同步重连（thundering herd）
		jittered := backoff + time.Duration(rand.Int64N(int64(backoff)/2)-int64(backoff)/4)
		select {
		case <-c.stop:
			return
		case <-time.After(jittered):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// Stop 停止网关并清理资源。
func (c *Client) Stop() {
	close(c.stop)
	if c.conn != nil {
		c.conn.Close()
	}
	<-c.stopped
}

func (c *Client) run() error {
	token, err := c.api.AccessToken()
	if err != nil {
		return fmt.Errorf("获取 access token: %w", err)
	}
	u, err := url.Parse(c.gatewayURL)
	if err != nil {
		return fmt.Errorf("解析网关地址: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("连接网关 %s: %w", u.Redacted(), err)
	}
	c.conn = conn
	defer conn.Close()
	if err := c.handshake(conn, token); err != nil {
		return err
	}
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if err := c.handle(conn, f, token); err != nil {
			return err
		}
	}
}

func (c *Client) handshake(conn *websocket.Conn, token string) error {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("握手读帧: %w", err)
		}
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Op == opHello {
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			json.Unmarshal(f.D, &hello)
			if hello.HeartbeatInterval > 0 {
				c.startHeartbeat(conn, hello.HeartbeatInterval)
			}
			payload := map[string]any{"token": "QQBot " + token}
			if c.sessionID != "" {
				payload["session_id"] = c.sessionID
				payload["seq"] = c.seq
				conn.WriteJSON(frame{Op: opResume, D: mustJSON(payload)})
			} else {
				payload["intents"] = c.intents
				payload["shard"] = c.shard
				conn.WriteJSON(frame{Op: opIdentify, D: mustJSON(payload)})
			}
			return nil
		}
	}
}

func (c *Client) startHeartbeat(conn *websocket.Conn, interval int) {
	if c.ping != nil {
		c.ping.Stop()
	}
	c.acked = true
	c.ping = time.NewTicker(time.Duration(interval) * time.Millisecond)
	go func() {
		for range c.ping.C {
			if !c.acked {
				conn.Close()
				return
			}
			c.acked = false
			conn.WriteJSON(frame{Op: opHeartbeat, D: mustJSON(map[string]any{"seq": c.seq})})
		}
	}()
}

func (c *Client) handle(conn *websocket.Conn, f frame, token string) error {
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	switch f.Op {
	case opHeartbeatACK:
		c.acked = true
	case opReconnect:
		return fmt.Errorf("服务端要求重连")
	case opInvalidSession:
		c.sessionID = ""
		c.seq = 0
		return fmt.Errorf("session 失效，需重新鉴权")
	case opDispatch:
		if f.T == "READY" {
			var ready struct {
				SessionID string `json:"session_id"`
			}
			json.Unmarshal(f.D, &ready)
			c.sessionID = ready.SessionID
			log.Printf("[gateway] 连接就绪，session=%s", ready.SessionID)
		}
		if f.T == "RESUMED" {
			log.Printf("[gateway] 会话恢复成功")
		}
		if f.S > c.seq {
			c.seq = f.S
		}
		if !constant.IsValidEventType(f.T) {
			return nil
		}
		c.dispatch(f)
	}
	return nil
}

func (c *Client) dispatch(f frame) {
	payload := structers.Payload{
		ID:        f.ID,
		Op:        f.Op,
		T:         f.T,
		EventType: constant.EventType(f.T),
	}
	if len(f.D) > 0 {
		if err := json.Unmarshal(f.D, &payload.Data); err != nil {
			log.Printf("[gateway] 解析事件数据失败 %s: %v", f.T, err)
			return
		}
	}
	middleware.ProcessPayload(payload, c.api)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
