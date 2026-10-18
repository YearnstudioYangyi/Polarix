package sshauth

import (
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/plugin"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestConfigValidation(t *testing.T) {
	if err := validateConfig(map[string]any{"network": "invalid"}); err == nil {
		t.Fatal("expected error for invalid network")
	}
	if err := validateConfig(map[string]any{"timeout_seconds": -1}); err == nil {
		t.Fatal("expected error for negative timeout")
	}
	if err := validateConfig(map[string]any{
		"network":         "tcp",
		"timeout_seconds": 30,
	}); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestApplyConfig(t *testing.T) {
	err := applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group",
		"socket_path":     "127.0.0.1:9999",
		"network":         "tcp",
		"allowed_users":   "user1, user2",
		"timeout_seconds": 45,
	})
	if err != nil {
		t.Fatalf("applyConfig failed: %v", err)
	}

	cfg := GetConfig()
	if !cfg.Enabled || cfg.GroupID != "test-group" || cfg.SocketPath != "127.0.0.1:9999" ||
		cfg.Network != "tcp" || len(cfg.AllowedUsers) != 2 || cfg.TimeoutSeconds != 45 {
		t.Fatalf("unexpected config values: %#v", cfg)
	}
}

func TestApprovalFlow(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
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
}

func TestDenialFlow(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
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

	// 主动断开连接，模拟网络中断或 qqauthd 重启
	_ = conn.Close()

	// 等待断线处理
	time.Sleep(200 * time.Millisecond)

	// 请求应当已被标记失效
	if !auth.resolved.Load() {
		t.Fatal("pendingAuth should be marked resolved/invalidated on disconnect")
	}

	// 消息应被撤回
	mu.Lock()
	count := len(*deletedMsgs)
	mu.Unlock()
	if count == 0 {
		t.Fatal("expected message to be recalled on disconnect, but no DELETE call recorded")
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

func TestIPCSendFailureProducesErrorFeedback(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	c := createTestClient(server.URL)

	// 创建一个没有有效连接的 pendingAuth
	auth := &pendingAuth{
		req: BotIPCRequest{
			ID: "test-send-fail",
		},
		conn: nil, // 空连接或已断开
	}
	pendingMap.Store(auth.req.ID, auth)

	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-fail", c)
	cbCtx.SetGroupId("test-group-id")
	cbCtx.Role = constant.RoleAdmin

	err := auth.resolve(true, cbCtx)
	if err == nil {
		t.Fatal("expected error when IPC connection is not active")
	}
}

func TestTimeoutFlowRecallsMessageAndDoesNotSendToIPC(t *testing.T) {
	server, deletedMsgs, mu := setupMockQQServer(t)
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
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
}

func TestProtocolVersionValidation(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
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
}

func TestDuplicateIDRejection(t *testing.T) {
	req := BotIPCRequest{
		Version: "1",
		ID:      "duplicate-id-test",
	}
	auth1 := &pendingAuth{req: req}
	pendingMap.Store(req.ID, auth1)
	defer pendingMap.Delete(req.ID)

	ipc := newIPCConnection(&net.TCPConn{})
	processApprovalRequest(req, ipc)

	// 原有请求没有被覆盖
	loaded, _ := pendingMap.Load(req.ID)
	if loaded != auth1 {
		t.Fatal("existing pending request was overwritten by duplicate ID")
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
