package gateway

import (
	"Plrx/lib/constant"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func newTestQQClient() *qqapi.Client {
	c := qqapi.Init("test-app-id", "test-secret", "https://api.bot.qq.com", requests.Init(1))
	c.SetAccessTokenForTest("mock-access-token", time.Now().Add(time.Hour))
	return &c
}

func setupMockWSServer(t *testing.T, handler func(conn *websocket.Conn)) (*httptest.Server, string) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func TestClientIdentifyAndHeartbeat(t *testing.T) {
	var identifyReceived atomic.Bool
	var heartbeatReceived atomic.Bool

	server, wsURL := setupMockWSServer(t, func(conn *websocket.Conn) {
		// 1. 发送 Hello
		helloMsg := map[string]any{
			"op": constant.OpCodeHello,
			"d": map[string]any{
				"heartbeat_interval": 100, // 100ms
			},
		}
		if err := conn.WriteJSON(helloMsg); err != nil {
			t.Errorf("write hello: %v", err)
			return
		}

		// 2. 接收 Identify
		var msg GatewayMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("read identify: %v", err)
			return
		}
		if msg.Op != constant.OpCodeIdentify {
			t.Errorf("expected Op 2, got %d", msg.Op)
		}
		var identify IdentifyData
		if err := json.Unmarshal(msg.D, &identify); err != nil {
			t.Errorf("unmarshal identify: %v", err)
		}
		if identify.Intents != 100663296 {
			t.Errorf("intents = %d, want 100663296", identify.Intents)
		}
		if len(identify.Shard) != 2 || identify.Shard[0] != 0 || identify.Shard[1] != 1 {
			t.Errorf("shard = %v, want [0 1]", identify.Shard)
		}
		identifyReceived.Store(true)

		// 3. 发送 Ready
		readyMsg := map[string]any{
			"op": constant.OpCodeDispatch,
			"s":  1,
			"t":  "READY",
			"d": map[string]any{
				"version":    1,
				"session_id": "test-session-123",
				"user": map[string]any{
					"id":       "bot-1",
					"username": "TestBot",
					"bot":      true,
				},
				"shard": [2]int{0, 1},
			},
		}
		if err := conn.WriteJSON(readyMsg); err != nil {
			t.Errorf("write ready: %v", err)
			return
		}

		// 4. 等待客户端心跳
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var hbMsg GatewayMessage
		if err := conn.ReadJSON(&hbMsg); err == nil {
			if hbMsg.Op == constant.OpCodeHeartbeat {
				heartbeatReceived.Store(true)
				// 发送 Heartbeat ACK
				_ = conn.WriteJSON(map[string]any{"op": constant.OpCodeHeartbeatACK})
			}
		}
	})
	defer server.Close()

	// 模拟 QQ API 客户端已具有有效 token
	qqClient := newTestQQClient()
	client := NewClient(Config{
		Gateway: wsURL,
		Intents: 100663296,
		Shard:   []int{0, 1},
	}, qqClient)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.connect()
	}()

	// 等待测试流程完成
	time.Sleep(300 * time.Millisecond)
	client.Stop()
	wg.Wait()

	if !identifyReceived.Load() {
		t.Errorf("identify was not received by server")
	}
	if !heartbeatReceived.Load() {
		t.Errorf("heartbeat was not received by server")
	}

	client.stateMu.RLock()
	sessionID := client.sessionID
	client.stateMu.RUnlock()
	if sessionID != "test-session-123" {
		t.Errorf("sessionID = %q, want 'test-session-123'", sessionID)
	}
}

func TestClientResume(t *testing.T) {
	var resumeReceived atomic.Bool

	server, wsURL := setupMockWSServer(t, func(conn *websocket.Conn) {
		// 1. 发送 Hello
		helloMsg := map[string]any{
			"op": constant.OpCodeHello,
			"d": map[string]any{
				"heartbeat_interval": 500,
			},
		}
		_ = conn.WriteJSON(helloMsg)

		// 2. 接收 Resume
		var msg GatewayMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("read resume: %v", err)
			return
		}
		if msg.Op != constant.OpCodeResume {
			t.Errorf("expected Op 6 Resume, got %d", msg.Op)
			return
		}
		var resume ResumeData
		if err := json.Unmarshal(msg.D, &resume); err != nil {
			t.Errorf("unmarshal resume: %v", err)
		}
		if resume.SessionID != "saved-sess-456" {
			t.Errorf("session_id = %q, want 'saved-sess-456'", resume.SessionID)
		}
		if resume.Seq != 88 {
			t.Errorf("seq = %d, want 88", resume.Seq)
		}
		resumeReceived.Store(true)

		// 3. 发送 RESUMED
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeDispatch,
			"s":  89,
			"t":  "RESUMED",
			"d":  "",
		})
	})
	defer server.Close()

	qqClient := newTestQQClient()
	client := NewClient(Config{
		Gateway: wsURL,
	}, qqClient)

	// 设置已存在的 session 和 seq
	client.sessionID = "saved-sess-456"
	client.latestSeq = 88
	client.canResume = true

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.connect()
	}()

	time.Sleep(200 * time.Millisecond)
	client.Stop()
	wg.Wait()

	if !resumeReceived.Load() {
		t.Errorf("resume was not received by server")
	}

	if atomic.LoadInt64(&client.latestSeq) != 89 {
		t.Errorf("latestSeq = %d, want 89", client.latestSeq)
	}
}

func TestClientInvalidSessionResetsState(t *testing.T) {
	server, wsURL := setupMockWSServer(t, func(conn *websocket.Conn) {
		// 发送 Hello
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeHello,
			"d":  map[string]any{"heartbeat_interval": 500},
		})

		// 读 Resume
		var msg GatewayMessage
		_ = conn.ReadJSON(&msg)

		// 发送 OpCode 9 Invalid Session
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeInvalidSession,
		})
	})
	defer server.Close()

	qqClient := newTestQQClient()
	client := NewClient(Config{Gateway: wsURL}, qqClient)
	client.sessionID = "old-session"
	client.latestSeq = 100
	client.canResume = true

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.connect()
	}()

	time.Sleep(200 * time.Millisecond)
	client.Stop()
	wg.Wait()

	client.stateMu.RLock()
	sessionID := client.sessionID
	canResume := client.canResume
	client.stateMu.RUnlock()

	if sessionID != "" {
		t.Errorf("expected sessionID to be cleared, got %q", sessionID)
	}
	if canResume {
		t.Errorf("expected canResume to be false")
	}
	if atomic.LoadInt64(&client.latestSeq) != 0 {
		t.Errorf("expected latestSeq to be reset to 0, got %d", client.latestSeq)
	}
}

func TestClientServerReconnect(t *testing.T) {
	server, wsURL := setupMockWSServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeHello,
			"d":  map[string]any{"heartbeat_interval": 500},
		})
		var msg GatewayMessage
		_ = conn.ReadJSON(&msg)

		// 发送 OpCode 7 Reconnect
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeReconnect,
		})
	})
	defer server.Close()

	qqClient := newTestQQClient()
	client := NewClient(Config{Gateway: wsURL}, qqClient)
	client.sessionID = "active-session"
	client.latestSeq = 50

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.connect()
	}()

	time.Sleep(200 * time.Millisecond)
	client.Stop()
	wg.Wait()

	client.stateMu.RLock()
	canResume := client.canResume
	sessionID := client.sessionID
	client.stateMu.RUnlock()

	if !canResume {
		t.Errorf("expected canResume to be true on OpCode 7 Reconnect")
	}
	if sessionID != "active-session" {
		t.Errorf("expected sessionID to be preserved, got %q", sessionID)
	}
}

func TestClientDispatchEventUpdatesSequence(t *testing.T) {
	server, wsURL := setupMockWSServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeHello,
			"d":  map[string]any{"heartbeat_interval": 500},
		})
		var msg GatewayMessage
		_ = conn.ReadJSON(&msg)

		// 发送业务事件
		_ = conn.WriteJSON(map[string]any{
			"op": constant.OpCodeDispatch,
			"s":  99,
			"t":  "GROUP_AT_MESSAGE_CREATE",
			"id": "event-99",
			"d": map[string]any{
				"id":           "msg-1",
				"content":      "/echo test",
				"group_openid": "group-123",
				"author": map[string]any{
					"member_openid": "member-123",
					"username":      "Alice",
				},
			},
		})
	})
	defer server.Close()

	qqClient := newTestQQClient()
	client := NewClient(Config{Gateway: wsURL}, qqClient)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.connect()
	}()

	time.Sleep(250 * time.Millisecond)
	client.Stop()
	wg.Wait()

	if seq := atomic.LoadInt64(&client.latestSeq); seq != 99 {
		t.Errorf("latestSeq = %d, want 99", seq)
	}
}

func TestResolveGatewayURL(t *testing.T) {
	tests := []struct {
		name       string
		rawGateway string
		proxyAPI   string
		want       string
	}{
		{
			name:       "empty proxyAPI",
			rawGateway: "wss://api.sgroup.qq.com/websocket",
			proxyAPI:   "",
			want:       "wss://api.sgroup.qq.com/websocket",
		},
		{
			name:       "official qq.com proxyAPI",
			rawGateway: "wss://api.sgroup.qq.com/websocket",
			proxyAPI:   "https://api.bot.qq.com",
			want:       "wss://api.sgroup.qq.com/websocket",
		},
		{
			name:       "http custom reverse proxy",
			rawGateway: "wss://api.sgroup.qq.com/websocket",
			proxyAPI:   "http://server.yearnstudio.cn:43567",
			want:       "ws://server.yearnstudio.cn:43567/websocket",
		},
		{
			name:       "https custom reverse proxy",
			rawGateway: "wss://api.sgroup.qq.com/websocket",
			proxyAPI:   "https://proxy.example.com:8443",
			want:       "wss://proxy.example.com:8443/websocket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveGatewayURL(tt.rawGateway, tt.proxyAPI)
			if got != tt.want {
				t.Errorf("resolveGatewayURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
