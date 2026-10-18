package sshauth

import (
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/message"
	"Plrx/lib/plugin"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupMockQQServer(t *testing.T) (*httptest.Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	deletedMsgs := make([]string, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.Method == http.MethodPost && r.URL.Path == "/v2/groups/test-group-id/messages" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				msgType, _ := body["msg_type"].(float64)
				if msgType == 0 {
					content, _ := body["content"].(string)
					if content == "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"message":  "消息content字段不能为空",
							"code":     40034030,
							"err_code": 40034030,
						})
						return
					}
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "msg-123456",
				"timestamp": time.Now().Format(time.RFC3339),
			})
			return
		}

		if r.Method == http.MethodDelete && r.URL.Path == "/v2/groups/test-group-id/messages/msg-123456" {
			deletedMsgs = append(deletedMsgs, "msg-123456")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPut && len(r.URL.Path) > len("/interactions/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	return server, &deletedMsgs, &mu
}

func createTestClient(serverURL string) *qqapi.Client {
	c := qqapi.Init("test-app", "test-secret", serverURL, requests.Init(1))
	c.SetAccessTokenForTest("test-token", time.Now().Add(time.Hour))
	return &c
}

func createUnixListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	// Windows 上 AF_UNIX 的 sun_path 最大长度为 108 字节（含 null 结尾符）。
	// t.TempDir() 会包含完整的长测试名称，可能导致路径超过限制，因此采用简短随机名。
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("t-ipc-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(sockPath)
	})

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket %s: %v", sockPath, err)
	}
	return listener, sockPath
}

func TestConfigValidation(t *testing.T) {
	if err := validateConfig(map[string]any{"network": "invalid"}); err == nil {
		t.Fatal("expected error for invalid network")
	}
	if err := validateConfig(map[string]any{"network": "tcp"}); err == nil {
		t.Fatal("expected error for tcp network: security design requires unix socket only")
	}
	if err := validateConfig(map[string]any{"timeout_seconds": -1}); err == nil {
		t.Fatal("expected error for negative timeout")
	}
	if err := validateConfig(map[string]any{
		"network":         "unix",
		"timeout_seconds": 30,
	}); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestApplyConfig(t *testing.T) {
	err := applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group",
		"socket_path":     "/tmp/test-socket.sock",
		"allowed_users":   "user1, user2",
		"timeout_seconds": 45,
	})
	if err != nil {
		t.Fatalf("applyConfig failed: %v", err)
	}

	cfg := GetConfig()
	if !cfg.Enabled || cfg.GroupID != "test-group" || cfg.SocketPath != "/tmp/test-socket.sock" ||
		len(cfg.AllowedUsers) != 2 || cfg.TimeoutSeconds != 45 {
		t.Fatalf("unexpected config values: %#v", cfg)
	}
}

func TestApprovalFlow(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, sockPath := createUnixListener(t)
	defer listener.Close()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     sockPath,
		"timeout_seconds": 60,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()

	req := BotIPCRequest{
		Version:    "1",
		User:       "root",
		Service:    "sshd",
		RemoteHost: "192.168.1.100",
		ID:         "test-req-approve-001",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	var auth *pendingAuth
	for range 50 {
		val, ok := pendingMap.Load(req.ID)
		if ok {
			auth = val.(*pendingAuth)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if auth == nil {
		t.Fatalf("pendingAuth not found in pendingMap")
	}

	// 模拟群管理员触发回调
	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-1", c)
	cbCtx.SetGroupId("test-group-id")
	cbCtx.SetUserId("admin-user")
	cbCtx.Role = constant.RoleAdmin
	cbCtx.ButtonId = "sshauth_approve_" + req.ID
	cbCtx.Data = "approve:" + req.ID

	if err := handleApprovalCallback(auth, true, cbCtx); err != nil {
		t.Fatalf("handleApprovalCallback error: %v", err)
	}

	var resp BotIPCResponse
	decoder := json.NewDecoder(conn)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response from plugin: %v", err)
	}

	if resp.Type != "approve" || resp.ID != req.ID || !resp.Approval {
		t.Fatalf("unexpected IPC response: %#v", resp)
	}

	if _, ok := pendingMap.Load(req.ID); ok {
		t.Fatalf("expected request %s to be deleted from pendingMap", req.ID)
	}

	if auth.status.Load() != int32(StatusDelivered) {
		t.Fatalf("expected status StatusDelivered, got %d", auth.status.Load())
	}

	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0, got %d", cnt)
	}
}

func TestDenialFlow(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, sockPath := createUnixListener(t)
	defer listener.Close()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     sockPath,
		"timeout_seconds": 60,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()

	req := BotIPCRequest{
		Version:    "1",
		User:       "attacker",
		Service:    "sshd",
		RemoteHost: "10.0.0.99",
		ID:         "test-req-deny-002",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	var auth *pendingAuth
	for range 50 {
		val, ok := pendingMap.Load(req.ID)
		if ok {
			auth = val.(*pendingAuth)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if auth == nil {
		t.Fatalf("pendingAuth not found in pendingMap")
	}

	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-2", c)
	cbCtx.SetGroupId("test-group-id")
	cbCtx.SetUserId("admin-user")
	cbCtx.Role = constant.RoleAdmin
	cbCtx.ButtonId = "sshauth_deny_" + req.ID
	cbCtx.Data = "deny:" + req.ID

	if err := handleApprovalCallback(auth, false, cbCtx); err != nil {
		t.Fatalf("handleApprovalCallback error: %v", err)
	}

	var resp BotIPCResponse
	decoder := json.NewDecoder(conn)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response from plugin: %v", err)
	}

	if resp.Type != "approve" || resp.ID != req.ID || resp.Approval {
		t.Fatalf("unexpected IPC response for denial: %#v", resp)
	}

	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0, got %d", cnt)
	}
}

func TestServerSideAuthorization(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	c := createTestClient(server.URL)
	_ = applyConfig(map[string]any{
		"enabled":       true,
		"group_id":      "valid-group-id",
		"allowed_users": "allowed-user-1, allowed-user-2",
	})

	auth := &pendingAuth{
		req: BotIPCRequest{
			ID: "test-auth-perm",
		},
	}

	// 1. 来源群不匹配
	cbWrongGroup := &context.CallbackContext{}
	cbWrongGroup.Init("evt-1", c)
	cbWrongGroup.SetGroupId("wrong-group-id")
	cbWrongGroup.SetUserId("allowed-user-1")
	cbWrongGroup.Role = constant.RoleAdmin
	_ = handleApprovalCallback(auth, true, cbWrongGroup)
	if auth.resolved.Load() {
		t.Fatal("request should not be resolved when group ID mismatches")
	}

	// 2. 白名单不匹配
	cbUnauthorizedUser := &context.CallbackContext{}
	cbUnauthorizedUser.Init("evt-2", c)
	cbUnauthorizedUser.SetGroupId("valid-group-id")
	cbUnauthorizedUser.SetUserId("stranger-user")
	cbUnauthorizedUser.Role = constant.RoleAdmin
	_ = handleApprovalCallback(auth, true, cbUnauthorizedUser)
	if auth.resolved.Load() {
		t.Fatal("request should not be resolved when user is not in allowed_users")
	}

	// 3. 切换为默认管理员模式（AllowedUsers 留空）
	_ = applyConfig(map[string]any{
		"enabled":       true,
		"group_id":      "valid-group-id",
		"allowed_users": "",
	})

	// 普通群员尝试点击
	cbMember := &context.CallbackContext{}
	cbMember.Init("evt-3", c)
	cbMember.SetGroupId("valid-group-id")
	cbMember.SetUserId("member-user")
	cbMember.Role = constant.RoleMember
	_ = handleApprovalCallback(auth, true, cbMember)
	if auth.resolved.Load() {
		t.Fatal("normal group member must NOT be allowed to approve SSH login")
	}
}

func TestDisconnectionInvalidatesPendingAndRecallsMessage(t *testing.T) {
	server, deletedMsgs, mu := setupMockQQServer(t)
	defer server.Close()

	listener, sockPath := createUnixListener(t)
	defer listener.Close()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     sockPath,
		"timeout_seconds": 60,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}

	req := BotIPCRequest{
		Version:    "1",
		User:       "user-disconnect",
		Service:    "sshd",
		RemoteHost: "192.168.1.88",
		ID:         "test-disconnect-001",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	var auth *pendingAuth
	for range 50 {
		val, ok := pendingMap.Load(req.ID)
		if ok {
			auth = val.(*pendingAuth)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if auth == nil {
		t.Fatalf("pendingAuth not found in pendingMap")
	}

	// 主动断开连接，模拟服务断开
	_ = conn.Close()

	// 等待断线处理
	time.Sleep(200 * time.Millisecond)

	// 请求应当已被标记失效
	if !auth.resolved.Load() {
		t.Fatal("pendingAuth should be marked resolved/invalidated on disconnect")
	}
	if auth.status.Load() != int32(StatusInvalidated) {
		t.Fatalf("expected status StatusInvalidated, got %d", auth.status.Load())
	}

	// 消息应被撤回
	mu.Lock()
	count := len(*deletedMsgs)
	mu.Unlock()
	if count == 0 {
		t.Fatal("expected message to be recalled on disconnect, but no DELETE call recorded")
	}

	// 计数应该归零
	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0 after disconnect, got %d", cnt)
	}

	// 断线后若有用户点击旧按钮，应该被拦截，且不向 IPC 发送
	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-late", c)
	cbCtx.SetGroupId("test-group-id")
	cbCtx.Role = constant.RoleAdmin
	err = handleApprovalCallback(auth, true, cbCtx)
	if err != nil {
		t.Fatalf("late callback unexpected error: %v", err)
	}
}

func TestIPCSendFailureProducesErrorFeedbackAndRecallsMessage(t *testing.T) {
	server, deletedMsgs, mu := setupMockQQServer(t)
	defer server.Close()

	c := createTestClient(server.URL)

	auth := &pendingAuth{
		req: BotIPCRequest{
			ID: "test-send-fail",
		},
		conn: nil, // 空连接或已断开
	}
	msg := &message.MarkdownMessage{
		Message: (&message.Message{
			Qapi:    c,
			GroupId: "test-group-id",
			Target:  constant.GroupMessage,
		}).InitRef(),
	}
	msg.Init()
	msg.MsgId = "msg-123456"
	auth.msg = msg

	pendingMap.Store(auth.req.ID, auth)
	pendingCounter.Store(1)

	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-fail", c)
	cbCtx.SetGroupId("test-group-id")
	cbCtx.Role = constant.RoleAdmin

	err := auth.resolve(true, cbCtx)
	if err == nil {
		t.Fatal("expected error when IPC connection is not active")
	}

	// 检查状态被标记为交付失败
	if auth.status.Load() != int32(StatusDeliveryFailed) {
		t.Fatalf("expected status StatusDeliveryFailed (%d), got %d", StatusDeliveryFailed, auth.status.Load())
	}

	// 验证群里的原审批消息被撤回，避免残留按钮误导用户
	mu.Lock()
	count := len(*deletedMsgs)
	mu.Unlock()
	if count == 0 {
		t.Fatal("expected message to be recalled when IPC send fails, but no DELETE request was sent")
	}

	// 验证待审批计数已归还
	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0 after failure cleanup, got %d", cnt)
	}

	// 再次点击应被拦截
	err = auth.resolve(true, cbCtx)
	if err != nil {
		t.Fatalf("subsequent resolve should return nil: %v", err)
	}
}

func TestTimeoutFlowRecallsMessageAndDoesNotSendToIPC(t *testing.T) {
	server, deletedMsgs, mu := setupMockQQServer(t)
	defer server.Close()

	listener, sockPath := createUnixListener(t)
	defer listener.Close()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     sockPath,
		"timeout_seconds": 1,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()

	req := BotIPCRequest{
		Version:    "1",
		User:       "user-timeout",
		Service:    "sshd",
		RemoteHost: "192.168.1.50",
		ID:         "test-req-timeout-003",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	count := len(*deletedMsgs)
	mu.Unlock()

	if count == 0 {
		t.Fatalf("expected message to be deleted on timeout, but no DELETE request was received")
	}

	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var resp BotIPCResponse
	decoder := json.NewDecoder(conn)
	err = decoder.Decode(&resp)
	if err == nil {
		t.Fatalf("expected no data on IPC on timeout, but received: %#v", resp)
	}

	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0 after timeout, got %d", cnt)
	}
}

func TestProtocolVersionValidation(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, sockPath := createUnixListener(t)
	defer listener.Close()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     sockPath,
		"timeout_seconds": 60,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()

	// 协议版本不是 "1"
	req := BotIPCRequest{
		Version: "2",
		User:    "root",
		ID:      "invalid-version-req",
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, ok := pendingMap.Load("invalid-version-req"); ok {
		t.Fatal("request with unsupported version should not be processed")
	}

	if cnt := pendingCounter.Load(); cnt != 0 {
		t.Fatalf("expected pendingCounter to be 0 for invalid version, got %d", cnt)
	}
}

func TestDuplicateIDRejection(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	c := createTestClient(server.URL)
	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"timeout_seconds": 60,
	})
	SetClient(c)

	req := BotIPCRequest{
		Version: "1",
		ID:      "duplicate-id-test",
	}
	auth1 := &pendingAuth{req: req}
	pendingMap.Store(req.ID, auth1)
	pendingCounter.Store(1)
	defer func() {
		pendingMap.Delete(req.ID)
		pendingCounter.Store(0)
	}()

	ipc := newIPCConnection(&net.UnixConn{})
	processApprovalRequest(req, ipc)

	// 原有请求没有被覆盖
	loaded, _ := pendingMap.Load(req.ID)
	if loaded != auth1 {
		t.Fatal("existing pending request was overwritten by duplicate ID")
	}

	// 待审批计数仍为 1，重复请求的名额被正确释放，没有造成计数泄露
	if cnt := pendingCounter.Load(); cnt != 1 {
		t.Fatalf("expected pendingCounter to remain 1 after duplicate ID rejection, got %d", cnt)
	}
}

func TestPendingCounterAtomicQuotaLimit(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	c := createTestClient(server.URL)
	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"timeout_seconds": 5,
	})
	SetClient(c)

	t.Cleanup(func() {
		pendingMap.Range(func(key, value any) bool {
			if auth, ok := value.(*pendingAuth); ok {
				auth.invalidate("测试退出清理")
			}
			pendingMap.Delete(key)
			return true
		})
		pendingCounter.Store(0)
	})

	// 重置计数器
	pendingCounter.Store(0)
	pendingMap.Range(func(key, value any) bool {
		pendingMap.Delete(key)
		return true
	})

	ipc := newIPCConnection(&net.UnixConn{})

	// 并发发送 150 个请求（上限是 MaxPendingRequests = 100）
	var wg sync.WaitGroup
	totalRequests := 150

	for i := range totalRequests {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := BotIPCRequest{
				Version: "1",
				ID:      fmt.Sprintf("limit-test-%03d", idx),
				User:    "test-user",
			}
			processApprovalRequest(req, ipc)
		}(i)
	}
	wg.Wait()

	// 验证最终成功保留的名额恰好等于 MaxPendingRequests (100)
	var acceptedCount int
	pendingMap.Range(func(key, value any) bool {
		acceptedCount++
		return true
	})

	if acceptedCount != MaxPendingRequests {
		t.Fatalf("expected exactly %d accepted requests in pendingMap, got %d", MaxPendingRequests, acceptedCount)
	}

	if cur := pendingCounter.Load(); cur != MaxPendingRequests {
		t.Fatalf("expected pendingCounter to be %d after all requests complete, got %d", MaxPendingRequests, cur)
	}

	// 逐个完成所有待审批请求，验证名额全部释放归零（无泄露）
	pendingMap.Range(func(key, value any) bool {
		if auth, ok := value.(*pendingAuth); ok {
			auth.invalidate("测试完成清理")
		}
		return true
	})

	if cur := pendingCounter.Load(); cur != 0 {
		t.Fatalf("expected pendingCounter to return to 0 after cleanup, got %d", cur)
	}
}

func TestMessageRecalledIfInvalidatedDuringSend(t *testing.T) {
	sendInFlightCh := make(chan struct{})
	finishSendCh := make(chan struct{})
	var deletedList []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.Method == http.MethodPost && r.URL.Path == "/v2/groups/test-group-id/messages" {
			// 通知外部测试：发送已经发起
			select {
			case sendInFlightCh <- struct{}{}:
			default:
			}

			// 等待外部将请求 invalidate
			mu.Unlock()
			<-finishSendCh
			mu.Lock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "msg-in-flight-001",
				"timestamp": time.Now().Format(time.RFC3339),
			})
			return
		}

		if r.Method == http.MethodDelete && r.URL.Path == "/v2/groups/test-group-id/messages/msg-in-flight-001" {
			deletedList = append(deletedList, "msg-in-flight-001")
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := createTestClient(server.URL)
	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"timeout_seconds": 60,
	})
	SetClient(c)

	ipc := newIPCConnection(&net.UnixConn{})
	req := BotIPCRequest{
		Version: "1",
		ID:      "test-in-flight-req",
		User:    "in-flight-user",
	}

	go processApprovalRequest(req, ipc)

	// 等待发送进入处理中
	<-sendInFlightCh

	// 此时请求正在向 QQ 发送，尚未返回 MsgId
	val, ok := pendingMap.Load(req.ID)
	if !ok {
		t.Fatalf("request not found in pendingMap")
	}
	auth := val.(*pendingAuth)

	// 模拟连接断开或服务重启，调用 invalidate
	auth.invalidate("发送过程中连接断开")

	// 允许发送完成并返回 MsgId
	close(finishSendCh)

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	// 验证刚发出的消息已被撤回
	mu.Lock()
	count := len(deletedList)
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected newly sent message to be recalled after invalidation during send, but no DELETE was called")
	}

	// 定时器应未启动
	auth.mu.Lock()
	timer := auth.timer
	auth.mu.Unlock()
	if timer != nil {
		t.Fatal("timer should NOT be started if request was already invalidated during send")
	}
}

func TestUnixSocketCommunication(t *testing.T) {
	sockPath := fmt.Sprintf(`%s\test-ipc-%d.sock`, os.TempDir(), time.Now().UnixNano())
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("unix domain sockets not supported on this platform: %v", err)
		return
	}
	defer listener.Close()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial unix socket: %v", err)
	}
	defer conn.Close()

	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept on unix socket: %v", err)
	}
	defer accepted.Close()
}

func TestPluginRegistration(t *testing.T) {
	managed, ok := plugin.ManagedPluginByID(PluginID)
	if !ok {
		t.Fatalf("plugin %s not found in managed plugins", PluginID)
	}

	if managed.Name != "SSH 登录认证" {
		t.Fatalf("unexpected name: %s", managed.Name)
	}

	foundCmd := false
	for _, cmd := range managed.Commands {
		if cmd == "/sshauth" {
			foundCmd = true
			break
		}
	}
	if !foundCmd {
		t.Fatalf("command /sshauth not found in managed commands: %v", managed.Commands)
	}
}
