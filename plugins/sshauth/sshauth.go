package sshauth

import (
	"Plrx/lib/buttons"
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/message"
	"Plrx/lib/plugin"
	"Plrx/lib/qqapi"
	"Plrx/lib/templates"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	PluginID               = "sshauth"
	DefaultSocketPath      = "/tmp/botipc-dev.sock"
	DefaultNetwork         = "unix"
	DefaultMode            = "connect" // "connect" (dial to ssh-qqauth) or "listen" (listen for connections)
	DefaultTimeoutSeconds = 60
)

// BotIPCRequest matches the request sent by ssh-qqauth
type BotIPCRequest struct {
	Version    string `json:"version"`
	User       string `json:"user"`
	Service    string `json:"service"`
	RemoteHost string `json:"remote_host"`
	ID         string `json:"id"`
}

// BotIPCResponse matches the response expected by ssh-qqauth
type BotIPCResponse struct {
	Version  string `json:"version"`
	Type     string `json:"type"`
	ID       string `json:"id"`
	Approval bool   `json:"approval"`
}

type Config struct {
	Enabled        bool
	GroupID        string
	SocketPath     string
	Network        string
	Mode           string
	Permission     string
	AllowedUsers   []string
	TimeoutSeconds int
}

type ipcWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	conn    net.Conn
}

func newIPCWriter(conn net.Conn) *ipcWriter {
	return &ipcWriter{
		encoder: json.NewEncoder(conn),
		conn:    conn,
	}
}

func (w *ipcWriter) SendResponse(resp BotIPCResponse) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(resp)
}

type pendingAuth struct {
	req       BotIPCRequest
	msg       *message.MarkdownMessage
	writer    *ipcWriter
	timer     *time.Timer
	resolved  atomic.Bool
	createdAt time.Time
}

var (
	stateMu sync.RWMutex
	client  *qqapi.Client
	config  Config = Config{
		Enabled:        true,
		SocketPath:     DefaultSocketPath,
		Network:        DefaultNetwork,
		Mode:           DefaultMode,
		Permission:     "admin",
		TimeoutSeconds: DefaultTimeoutSeconds,
	}

	workerMu       sync.Mutex
	workerStopCh   chan struct{}
	activeListener net.Listener

	pendingMap sync.Map // key: request ID (string), value: *pendingAuth
)

func init() {
	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "SSH 登录认证",
		Description: "监听 ssh-qqauth IPC 通信，并在配置的群聊中进行授权审批与回调",
		Config: []plugin.ConfigField{
			{
				Key:         "enabled",
				Label:       "启用插件",
				Description: "是否开启 SSH 认证审批插件",
				Type:        plugin.ConfigFieldTypeBoolean,
			},
			{
				Key:         "group_id",
				Label:       "授权群 OpenID",
				Description: "接收并审批 SSH 登录请求的目标群聊 OpenID",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: "群 OpenID",
			},
			{
				Key:         "socket_path",
				Label:       "IPC Socket 地址",
				Description: "Unix Socket 路径或 TCP 地址 (如 /tmp/botipc-dev.sock 或 127.0.0.1:9090)",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: DefaultSocketPath,
			},
			{
				Key:         "network",
				Label:       "网络类型",
				Description: "unix 或 tcp (默认为 unix)",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: DefaultNetwork,
			},
			{
				Key:         "mode",
				Label:       "连接模式",
				Description: "connect (主动连接 ssh-qqauth) 或 listen (监听连接，默认 connect)",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: DefaultMode,
			},
			{
				Key:         "permission",
				Label:       "审批按钮权限",
				Description: "admin (仅管理员) 或 all (群成员均可，默认 admin)",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: "admin",
			},
			{
				Key:         "allowed_users",
				Label:       "指定审批用户",
				Description: "允许审批的用户 OpenID，多个用英文逗号分隔（留空则遵循按钮权限）",
				Type:        plugin.ConfigFieldTypeText,
			},
			{
				Key:         "timeout_seconds",
				Label:       "超时秒数",
				Description: "审批超时时间（秒，默认 60 秒）",
				Type:        plugin.ConfigFieldTypeInt,
				Placeholder: "60",
			},
		},
		ValidateConfig: validateConfig,
		ApplyConfig:    applyConfig,
		Commands: []*plugin.Command{
			{
				Prefix:   "/sshauth",
				Role:     constant.RoleAdmin,
				Describe: "查看 SSH 认证插件状态及待处理请求",
				Handle:   statusCommand,
			},
		},
	})
}

// SetClient sets the QQ API client and starts or restarts the IPC worker if enabled.
func SetClient(c *qqapi.Client) {
	stateMu.Lock()
	client = c
	stateMu.Unlock()

	restartWorker()
}

func GetConfig() Config {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return config
}

func parseAllowedUsers(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func validateConfig(values map[string]any) error {
	network, _ := values["network"].(string)
	network = strings.ToLower(strings.TrimSpace(network))
	if network != "" && network != "unix" && network != "tcp" {
		return fmt.Errorf("network 必须是 'unix' 或 'tcp'")
	}

	mode, _ := values["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != "connect" && mode != "listen" {
		return fmt.Errorf("mode 必须是 'connect' 或 'listen'")
	}

	permission, _ := values["permission"].(string)
	permission = strings.ToLower(strings.TrimSpace(permission))
	if permission != "" && permission != "admin" && permission != "all" {
		return fmt.Errorf("permission 必须是 'admin' 或 'all'")
	}

	timeout, _ := values["timeout_seconds"].(int)
	if timeout < 0 {
		return fmt.Errorf("timeout_seconds 不能为负数")
	}

	return nil
}

func applyConfig(values map[string]any) error {
	stateMu.Lock()
	next := Config{
		Enabled:        true,
		SocketPath:     DefaultSocketPath,
		Network:        DefaultNetwork,
		Mode:           DefaultMode,
		Permission:     "admin",
		TimeoutSeconds: DefaultTimeoutSeconds,
	}

	if enabled, ok := values["enabled"].(bool); ok {
		next.Enabled = enabled
	}
	if groupID, ok := values["group_id"].(string); ok {
		next.GroupID = strings.TrimSpace(groupID)
	}
	if socketPath, ok := values["socket_path"].(string); ok && strings.TrimSpace(socketPath) != "" {
		next.SocketPath = strings.TrimSpace(socketPath)
	}
	if network, ok := values["network"].(string); ok && strings.TrimSpace(network) != "" {
		next.Network = strings.ToLower(strings.TrimSpace(network))
	}
	if mode, ok := values["mode"].(string); ok && strings.TrimSpace(mode) != "" {
		next.Mode = strings.ToLower(strings.TrimSpace(mode))
	}
	if perm, ok := values["permission"].(string); ok && strings.TrimSpace(perm) != "" {
		next.Permission = strings.ToLower(strings.TrimSpace(perm))
	}
	if allowedUsers, ok := values["allowed_users"].(string); ok {
		next.AllowedUsers = parseAllowedUsers(allowedUsers)
	}
	if timeout, ok := values["timeout_seconds"].(int); ok && timeout > 0 {
		next.TimeoutSeconds = timeout
	}

	config = next
	stateMu.Unlock()

	restartWorker()
	return nil
}

func restartWorker() {
	workerMu.Lock()
	defer workerMu.Unlock()

	if workerStopCh != nil {
		close(workerStopCh)
		workerStopCh = nil
	}
	if activeListener != nil {
		_ = activeListener.Close()
		activeListener = nil
	}

	stateMu.RLock()
	cfg := config
	hasClient := (client != nil)
	stateMu.RUnlock()

	if !cfg.Enabled || !hasClient {
		return
	}

	workerStopCh = make(chan struct{})
	stopCh := workerStopCh

	if cfg.Mode == "listen" {
		go runListener(cfg, stopCh)
	} else {
		go runConnector(cfg, stopCh)
	}
}

func runListener(cfg Config, stopCh <-chan struct{}) {
	if cfg.Network == "unix" {
		_ = os.Remove(cfg.SocketPath)
	}

	listener, err := net.Listen(cfg.Network, cfg.SocketPath)
	if err != nil {
		log.Printf("[sshauth] failed to listen on %s (%s): %v", cfg.SocketPath, cfg.Network, err)
		return
	}
	defer func() {
		_ = listener.Close()
		if cfg.Network == "unix" {
			_ = os.Remove(cfg.SocketPath)
		}
	}()

	workerMu.Lock()
	activeListener = listener
	workerMu.Unlock()

	log.Printf("[sshauth] listening for IPC connections on %s (%s)", cfg.SocketPath, cfg.Network)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				log.Printf("[sshauth] accept IPC error: %v", err)
				return
			}
		}

		go handleConnection(conn, stopCh)
	}
}

func runConnector(cfg Config, stopCh <-chan struct{}) {
	log.Printf("[sshauth] IPC connector started for %s (%s)", cfg.SocketPath, cfg.Network)

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		conn, err := net.DialTimeout(cfg.Network, cfg.SocketPath, 3*time.Second)
		if err != nil {
			select {
			case <-stopCh:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		log.Printf("[sshauth] successfully connected to IPC server at %s", cfg.SocketPath)
		handleConnection(conn, stopCh)

		log.Printf("[sshauth] disconnected from IPC server, reconnecting...")
		select {
		case <-stopCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func handleConnection(conn net.Conn, stopCh <-chan struct{}) {
	defer conn.Close()

	writer := newIPCWriter(conn)
	decoder := json.NewDecoder(conn)

	// Close conn when stopCh triggers
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		select {
		case <-stopCh:
			_ = conn.Close()
		case <-doneCh:
		}
	}()

	for {
		var req BotIPCRequest
		if err := decoder.Decode(&req); err != nil {
			if err != io.EOF {
				select {
				case <-stopCh:
				default:
					log.Printf("[sshauth] IPC decode error: %v", err)
				}
			}
			return
		}

		go processApprovalRequest(req, writer)
	}
}

func processApprovalRequest(req BotIPCRequest, writer *ipcWriter) {
	if req.ID == "" {
		log.Printf("[sshauth] received request without ID, ignoring")
		return
	}

	stateMu.RLock()
	cfg := config
	c := client
	stateMu.RUnlock()

	if !cfg.Enabled {
		log.Printf("[sshauth] plugin is disabled, ignoring request %s", req.ID)
		return
	}
	if c == nil {
		log.Printf("[sshauth] QQ API client not ready, cannot process request %s", req.ID)
		return
	}
	if cfg.GroupID == "" {
		log.Printf("[sshauth] target group_id is not configured, cannot send request %s", req.ID)
		return
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = DefaultTimeoutSeconds
	}

	user := req.User
	if user == "" {
		user = "未知用户"
	}
	service := req.Service
	if service == "" {
		service = "ssh"
	}
	remoteHost := req.RemoteHost
	if remoteHost == "" {
		remoteHost = "未知地址"
	}

	// 构造 Markdown 消息内容
	templateArgs := templates.Args{
		"user":        user,
		"service":     service,
		"remote_host": remoteHost,
		"id":          req.ID,
		"timeout":     timeoutSec,
	}
	content, err := templates.FillMarkdownTemplate("SSHAuthRequest", templateArgs)
	if err != nil {
		content = fmt.Sprintf("# 🔔 SSH 登录授权申请\n\n"+
			"- **申请用户**: %s\n"+
			"- **请求服务**: %s\n"+
			"- **来源地址**: %s\n"+
			"- **请求编号**: `%s`\n"+
			"- **有效时间**: %d 秒\n\n"+
			"> 请相关管理员在 %d 秒内点击下方按钮进行审批。超时将自动失效并撤回此消息。",
			user, service, remoteHost, req.ID, timeoutSec, timeoutSec)
	}

	auth := &pendingAuth{
		req:       req,
		writer:    writer,
		createdAt: time.Now(),
	}

	// 构造交互按钮
	k := &buttons.Keyboard{}
	btnApprove, errApprove := k.AppendButton("sshauth_approve_"+req.ID, "批准登录", "已批准", buttons.Blue, 0)
	if errApprove != nil {
		log.Printf("[sshauth] failed to create approve button: %v", errApprove)
		return
	}
	btnApprove.SetCallback("approve:"+req.ID, func(cbCtx *context.CallbackContext) error {
		return auth.resolve(true, cbCtx)
	})

	btnDeny, errDeny := k.AppendButton("sshauth_deny_"+req.ID, "拒绝登录", "已拒绝", buttons.Gray, 0)
	if errDeny != nil {
		log.Printf("[sshauth] failed to create deny button: %v", errDeny)
		return
	}
	btnDeny.SetCallback("deny:"+req.ID, func(cbCtx *context.CallbackContext) error {
		return auth.resolve(false, cbCtx)
	})

	// 设置权限（直接索引切片，防止切片扩容导致指针失效）
	approveBtn := &k.Rows[0].List[0]
	denyBtn := &k.Rows[0].List[1]
	if len(cfg.AllowedUsers) > 0 {
		approveBtn.SetUserWhiteList(cfg.AllowedUsers)
		denyBtn.SetUserWhiteList(cfg.AllowedUsers)
	} else if cfg.Permission == "all" {
		approveBtn.SetPermission(buttons.AllUser)
		denyBtn.SetPermission(buttons.AllUser)
	} else {
		approveBtn.SetPermission(buttons.Admin)
		denyBtn.SetPermission(buttons.Admin)
	}

	// 发送主动群消息
	msg := &message.MarkdownMessage{
		Message: (&message.Message{
			Qapi:    c,
			GroupId: cfg.GroupID,
			Target:  constant.GroupMessage,
		}).InitRef(),
	}
	msg.Init()
	msg.SetInitiativeMessage()
	msg.Content(content)
	msg.Keyboard(k)

	auth.msg = msg

	if err := msg.Send(); err != nil {
		log.Printf("[sshauth] failed to send approval message for %s to group %s: %v", req.ID, cfg.GroupID, err)
		return
	}
	log.Printf("[sshauth] sent approval message for request %s (msg_id: %s) to group %s", req.ID, msg.MsgId, cfg.GroupID)

	// 存入 pendingMap
	pendingMap.Store(req.ID, auth)

	// 启动超时定时器：超时则撤回消息，不向 IPC 传递
	auth.timer = time.AfterFunc(time.Duration(timeoutSec)*time.Second, func() {
		auth.onTimeout()
	})
}

func (a *pendingAuth) resolve(approved bool, cbCtx *context.CallbackContext) error {
	if a.timer != nil {
		a.timer.Stop()
	}

	if !a.resolved.CompareAndSwap(false, true) {
		// 已经处理或已超时
		if cbCtx != nil {
			_ = cbCtx.Done()
		}
		return nil
	}

	pendingMap.Delete(a.req.ID)

	if cbCtx != nil {
		_ = cbCtx.Done()
	}

	resp := BotIPCResponse{
		Version:  "1",
		Type:     "approve",
		ID:       a.req.ID,
		Approval: approved,
	}

	if a.writer != nil {
		if err := a.writer.SendResponse(resp); err != nil {
			log.Printf("[sshauth] failed to pass response to IPC for %s: %v", a.req.ID, err)
		} else {
			log.Printf("[sshauth] successfully passed response to IPC for %s: approval=%v", a.req.ID, approved)
		}
	} else {
		log.Printf("[sshauth] IPC connection writer is nil, cannot send response for %s", a.req.ID)
	}

	if cbCtx != nil {
		actionText := "批准"
		if !approved {
			actionText = "拒绝"
		}
		_ = cbCtx.Text(fmt.Sprintf("已%s来自 %s 的 %s 登录申请 (ID: %s)", actionText, a.req.RemoteHost, a.req.User, a.req.ID)).Send()
	}

	return nil
}

func (a *pendingAuth) onTimeout() {
	if !a.resolved.CompareAndSwap(false, true) {
		// 已经处理过了
		return
	}

	pendingMap.Delete(a.req.ID)
	log.Printf("[sshauth] Request %s timed out after %d seconds. Recalling message (not passing to IPC)...", a.req.ID, config.TimeoutSeconds)

	if a.msg != nil && a.msg.MsgId != "" {
		if err := a.msg.DeleteMessage(); err != nil {
			log.Printf("[sshauth] failed to recall timed-out message %s for request %s: %v", a.msg.MsgId, a.req.ID, err)
		} else {
			log.Printf("[sshauth] successfully recalled timed-out message %s for request %s", a.msg.MsgId, a.req.ID)
		}
	}
	// 超时则撤回消息，不向 ipc 传递 (ipc 那边会自动处理)
}

func statusCommand(ctx *context.MessageContext) error {
	stateMu.RLock()
	cfg := config
	hasClient := (client != nil)
	stateMu.RUnlock()

	var pendingCount int
	var pendingInfo strings.Builder
	pendingMap.Range(func(key, value any) bool {
		pendingCount++
		if auth, ok := value.(*pendingAuth); ok {
			fmt.Fprintf(&pendingInfo, "\n- `%s`: %s@%s (已等待 %v)",
				auth.req.ID, auth.req.User, auth.req.RemoteHost,
				time.Since(auth.createdAt).Truncate(time.Second))
		}
		return true
	})

	statusText := "运行中"
	if !cfg.Enabled {
		statusText = "已禁用"
	} else if !hasClient {
		statusText = "未就绪 (等待 QQ Client)"
	}

	text := fmt.Sprintf("### 🛡️ SSH 登录认证插件状态\n\n"+
		"- **状态**: %s\n"+
		"- **授权群 OpenID**: `%s`\n"+
		"- **IPC 地址**: `%s` (%s / %s)\n"+
		"- **按钮权限**: `%s`\n"+
		"- **超时时间**: %d 秒\n"+
		"- **待审批请求数**: %d%s",
		statusText, cfg.GroupID, cfg.SocketPath, cfg.Network, cfg.Mode,
		cfg.Permission, cfg.TimeoutSeconds, pendingCount, pendingInfo.String())

	return ctx.Markdown(text).Send()
}
