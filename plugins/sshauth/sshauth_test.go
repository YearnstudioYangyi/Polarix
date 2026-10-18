package sshauth

import (
	"Plrx/lib/buttons"
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
	"sync/atomic"
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
	if err := validateConfig(map[string]any{"mode": "invalid"}); err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if err := validateConfig(map[string]any{"permission": "invalid"}); err == nil {
		t.Fatal("expected error for invalid permission")
	}
	if err := validateConfig(map[string]any{"timeout_seconds": -1}); err == nil {
		t.Fatal("expected error for negative timeout")
	}
	if err := validateConfig(map[string]any{
		"network":         "tcp",
		"mode":            "listen",
		"permission":      "admin",
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
		"mode":            "listen",
		"permission":      "all",
		"allowed_users":   "user1, user2",
		"timeout_seconds": 45,
	})
	if err != nil {
		t.Fatalf("applyConfig failed: %v", err)
	}

	cfg := GetConfig()
	if !cfg.Enabled || cfg.GroupID != "test-group" || cfg.SocketPath != "127.0.0.1:9999" ||
		cfg.Network != "tcp" || cfg.Mode != "listen" || cfg.Permission != "all" ||
		len(cfg.AllowedUsers) != 2 || cfg.TimeoutSeconds != 45 {
		t.Fatalf("unexpected config values: %#v", cfg)
	}
}

func TestApprovalFlow(t *testing.T) {
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	// Spin up a TCP listener simulating ssh-qqauth server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Configure the plugin to connect to our listener
	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
		"mode":            "connect",
		"permission":      "admin",
		"timeout_seconds": 60,
	})

	c := createTestClient(server.URL)
	SetClient(c)

	// Accept connection from plugin
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()

	// Send an approval request from "ssh-qqauth" to plugin
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

	// Wait for plugin to process and store in pendingMap
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

	// Now simulate approving via callback
	cbCtx := &context.CallbackContext{}
	cbCtx.Init("event-1", c)
	cbCtx.ButtonId = "sshauth_approve_" + req.ID
	cbCtx.Data = "approve:" + req.ID

	if err := auth.resolve(true, cbCtx); err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	// The plugin should write BotIPCResponse to conn
	var resp BotIPCResponse
	decoder := json.NewDecoder(conn)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response from plugin: %v", err)
	}

	if resp.Type != "approve" || resp.ID != req.ID || !resp.Approval {
		t.Fatalf("unexpected IPC response: %#v", resp)
	}

	// Should be removed from pendingMap
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
		"mode":            "connect",
		"permission":      "admin",
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
	cbCtx.ButtonId = "sshauth_deny_" + req.ID
	cbCtx.Data = "deny:" + req.ID

	if err := auth.resolve(false, cbCtx); err != nil {
		t.Fatalf("resolve error: %v", err)
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

func TestTimeoutFlowRecallsMessageAndDoesNotSendToIPC(t *testing.T) {
	server, deletedMsgs, mu := setupMockQQServer(t)
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Set timeout to 1 second for fast test
	_ = applyConfig(map[string]any{
		"enabled":         true,
		"group_id":        "test-group-id",
		"socket_path":     addr,
		"network":         "tcp",
		"mode":            "connect",
		"permission":      "admin",
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

	// Wait for timeout (1s) plus buffer
	time.Sleep(1500 * time.Millisecond)

	// Check that the message was recalled via DELETE
	mu.Lock()
	count := len(*deletedMsgs)
	mu.Unlock()

	if count == 0 {
		t.Fatalf("expected message to be deleted on timeout, but no DELETE request was received")
	}

	// Verify that NOTHING was sent over the IPC connection
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var resp BotIPCResponse
	decoder := json.NewDecoder(conn)
	err = decoder.Decode(&resp)
	if err == nil {
		t.Fatalf("expected no data on IPC on timeout, but received: %#v", resp)
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

func TestIdempotency(t *testing.T) {
	auth := &pendingAuth{
		req: BotIPCRequest{
			ID: "test-idempotent",
		},
	}

	// First resolution
	if !auth.resolved.CompareAndSwap(false, true) {
		t.Fatal("first CAS should succeed")
	}

	// Second resolution should fail
	if auth.resolved.CompareAndSwap(false, true) {
		t.Fatal("second CAS should fail")
	}
}

func TestButtonCallbackInvocation(t *testing.T) {
	// Test that button callback can be properly triggered through buttons.InvokeCallback
	server, _, _ := setupMockQQServer(t)
	defer server.Close()

	c := &qqapi.Client{
		ProxyAPI: server.URL,
		Request:  requests.Init(1),
	}

	var called atomic.Bool
	callbackExtID := buttons.RegisterCallbackFunc("sshauth_approve_test123", func(ctx *context.CallbackContext) error {
		called.Store(true)
		return nil
	})

	cbCtx := &context.CallbackContext{}
	cbCtx.Init("test-event", c)

	if err := buttons.InvokeCallback(callbackExtID, cbCtx); err != nil {
		t.Fatalf("InvokeCallback failed: %v", err)
	}

	if !called.Load() {
		t.Fatal("callback function was not executed")
	}
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
