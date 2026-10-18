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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	PluginID              = "sshauth"
	DefaultSocketPath     = "/tmp/botipc-dev.sock"
	SocketNetwork         = "unix"
	DefaultTimeoutSeconds = 60
	MaxPendingRequests    = 100
	IOWriteTimeout        = 3 * time.Second
)

type RequestStatus int32

const (
	StatusPending RequestStatus = iota
	StatusDelivered
	StatusDeliveryFailed
	StatusTimedOut
	StatusInvalidated
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
	AllowedUsers   []string
	TimeoutSeconds int
}

type ipcConnection struct {
	conn    net.Conn
	encoder *json.Encoder
	mu      sync.Mutex
	active  atomic.Bool
	pending sync.Map // map[string]*pendingAuth (associated with this connection)
}

func newIPCConnection(conn net.Conn) *ipcConnection {
	c := &ipcConnection{
		conn:    conn,
		encoder: json.NewEncoder(conn),
	}
	c.active.Store(true)
	return c
}

func (c *ipcConnection) SendResponse(resp BotIPCResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.active.Load() {
		return fmt.Errorf("IPC connection is closed")
	}

	if err := c.conn.SetWriteDeadline(time.Now().Add(IOWriteTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	defer c.conn.SetWriteDeadline(time.Time{})

	return c.encoder.Encode(resp)
}

func (c *ipcConnection) Close() {
	if c.active.CompareAndSwap(true, false) {
		_ = c.conn.Close()
	}
}

func (c *ipcConnection) cleanupAllPending(reason string) {
	c.pending.Range(func(key, value any) bool {
		if auth, ok := value.(*pendingAuth); ok {
			auth.invalidate(reason)
		}
		c.pending.Delete(key)
		return true
	})
}

type pendingAuth struct {
	req       BotIPCRequest
	msg       *message.MarkdownMessage
	conn      *ipcConnection
	timer     *time.Timer
	resolved  atomic.Bool
	status    atomic.Int32
	createdAt time.Time
	mu        sync.Mutex
}

func (a *pendingAuth) cleanup() {
	a.mu.Lock()
	if a.timer != nil {
		a.timer.Stop()
	}
	a.mu.Unlock()

	pendingMap.Delete(a.req.ID)
	if a.conn != nil {
		a.conn.pending.Delete(a.req.ID)
	}
	pendingCounter.Add(-1)
}

var (
	stateMu sync.RWMutex
	client  *qqapi.Client
	config  Config = Config{
		Enabled:        true,
		SocketPath:     DefaultSocketPath,
		TimeoutSeconds: DefaultTimeoutSeconds,
	}

	workerMu     sync.Mutex
	workerStopCh chan struct{}
	currentConn  *ipcConnection

	pendingMap     sync.Map // key: request ID (string), value: *pendingAuth
	pendingCounter atomic.Int32
)

func init() {
	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "SSH 登录认证",
		Description: "主动连接 ssh-qqauth IPC 通信，并在配置的群聊中进行授权审批与回调",
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
				Label:       "IPC Socket 路径",
				Description: "Unix Domain Socket 路径 (如 /tmp/botipc-dev.sock)",
				Type:        plugin.ConfigFieldTypeText,
				Placeholder: DefaultSocketPath,
			},
			{
				Key:         "allowed_users",
				Label:       "指定审批用户",
				Description: "允许审批的用户 OpenID，多个用英文逗号分隔（留空则默认仅群主和群管理员可审批）",
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

// SetClient sets the QQ API client and starts or restarts the IPC connector worker.
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
	if netVal, ok := values["network"].(string); ok && strings.TrimSpace(netVal) != "" {
		if strings.ToLower(strings.TrimSpace(netVal)) != SocketNetwork {
			return fmt.Errorf("仅支持 Unix Domain Socket 通信，不支持网络类型 %q", netVal)
		}
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
	if currentConn != nil {
		currentConn.Close()
		currentConn.cleanupAllPending("配置更新或服务重启")
		currentConn = nil
	}

	// 清理全局残留请求并撤回群消息
	pendingMap.Range(func(key, value any) bool {
		if auth, ok := value.(*pendingAuth); ok {
			auth.invalidate("配置更新或服务重启")
		}
		pendingMap.Delete(key)
		return true
	})
	pendingCounter.Store(0)

	stateMu.RLock()
	cfg := config
	hasClient := (client != nil)
	stateMu.RUnlock()

	if !cfg.Enabled || !hasClient {
		return
	}

	workerStopCh = make(chan struct{})
	go runConnector(cfg, workerStopCh)
}

func runConnector(cfg Config, stopCh <-chan struct{}) {
	log.Printf("[sshauth] IPC connector worker started for %s (unix)", cfg.SocketPath)

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		conn, err := net.DialTimeout(SocketNetwork, cfg.SocketPath, 3*time.Second)
		if err != nil {
			select {
			case <-stopCh:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		ipc := newIPCConnection(conn)
		workerMu.Lock()
		currentConn = ipc
		workerMu.Unlock()

		log.Printf("[sshauth] successfully connected to IPC server at %s (unix)", cfg.SocketPath)
		handleConnection(ipc, stopCh)

		workerMu.Lock()
		if currentConn == ipc {
			currentConn = nil
		}
		workerMu.Unlock()

		ipc.Close()
		ipc.cleanupAllPending("与 SSH 认证服务断开连接")

		log.Printf("[sshauth] disconnected from IPC server, reconnecting in 2s...")
		select {
		case <-stopCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func handleConnection(ipc *ipcConnection, stopCh <-chan struct{}) {
	decoder := json.NewDecoder(ipc.conn)

	// Close conn when stopCh triggers
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		select {
		case <-stopCh:
			ipc.Close()
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

		// 协议校验
		if req.Version != "1" {
			log.Printf("[sshauth] unsupported protocol version %q from IPC, ignoring request", req.Version)
			continue
		}
		if req.ID == "" || len(req.ID) > 128 {
			log.Printf("[sshauth] invalid or oversized request ID %q, ignoring request", req.ID)
			continue
		}

		go processApprovalRequest(req, ipc)
	}
}

func processApprovalRequest(req BotIPCRequest, ipc *ipcConnection) {
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

	// 1. 先原子预留容量，再决定是否继续
	if pendingCounter.Add(1) > MaxPendingRequests {
		pendingCounter.Add(-1)
		log.Printf("[sshauth] too many pending requests (> %d), dropping request %s", MaxPendingRequests, req.ID)
		return
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = DefaultTimeoutSeconds
	}

	auth := &pendingAuth{
		req:       req,
		conn:      ipc,
		createdAt: time.Now(),
	}

	// 2. 尝试原子注册到全局 pendingMap，防止同 ID 冲突
	if _, loaded := pendingMap.LoadOrStore(req.ID, auth); loaded {
		pendingCounter.Add(-1)
		log.Printf("[sshauth] duplicate pending request ID %s, ignoring", req.ID)
		return
	}
	ipc.pending.Store(req.ID, auth)

	// 如果 IPC 连接已关闭，立即作废该请求
	if !ipc.active.Load() {
		auth.invalidate("与 SSH 认证服务断开连接")
		return
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

	// 构造交互按钮
	k := &buttons.Keyboard{}
	_, errApprove := k.AppendButton("sshauth_approve_"+req.ID, "批准登录", "已批准", buttons.Blue, 0)
	if errApprove != nil {
		auth.invalidate("创建批准按钮失败")
		log.Printf("[sshauth] failed to create approve button: %v", errApprove)
		return
	}

	_, errDeny := k.AppendButton("sshauth_deny_"+req.ID, "拒绝登录", "已拒绝", buttons.Gray, 0)
	if errDeny != nil {
		auth.invalidate("创建拒绝按钮失败")
		log.Printf("[sshauth] failed to create deny button: %v", errDeny)
		return
	}

	// 必须在所有按钮追加完成后获取切片内的稳定指针，防止切片扩容导致指针失效
	approveBtn := &k.Rows[0].List[0]
	denyBtn := &k.Rows[0].List[1]

	// 注册回调（包含服务端严格权限验证）
	approveBtn.SetCallback("approve:"+req.ID, func(cbCtx *context.CallbackContext) error {
		return handleApprovalCallback(auth, true, cbCtx)
	})
	denyBtn.SetCallback("deny:"+req.ID, func(cbCtx *context.CallbackContext) error {
		return handleApprovalCallback(auth, false, cbCtx)
	})

	// 设置前端展示权限
	if len(cfg.AllowedUsers) > 0 {
		approveBtn.SetUserWhiteList(cfg.AllowedUsers)
		denyBtn.SetUserWhiteList(cfg.AllowedUsers)
	} else {
		// 生产环境默认且仅支持管理员
		approveBtn.SetPermission(buttons.Admin)
		denyBtn.SetPermission(buttons.Admin)
	}

	// 构造主动群消息
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

	// 发送消息
	if err := msg.Send(); err != nil {
		auth.invalidate("群消息发送失败")
		log.Printf("[sshauth] failed to send approval message for %s to group %s: %v", req.ID, cfg.GroupID, err)
		return
	}

	// 消息发送成功后，检查请求是否在此期间已被废弃（例如连接断开或服务重启）
	auth.mu.Lock()
	auth.msg = msg
	if auth.resolved.Load() {
		auth.mu.Unlock()
		log.Printf("[sshauth] request %s was already resolved/invalidated during sending, recalling message %s", req.ID, msg.MsgId)
		if msg.MsgId != "" {
			_ = msg.DeleteMessage()
		}
		return
	}

	// 将审批超时与“消息发送完成后的有效审批窗口”对应起来，此时启动定时器
	auth.timer = time.AfterFunc(time.Duration(timeoutSec)*time.Second, auth.onTimeout)
	auth.mu.Unlock()

	log.Printf("[sshauth] sent approval message for request %s (msg_id: %s) to group %s", req.ID, msg.MsgId, cfg.GroupID)
}

// handleApprovalCallback 在执行具体审批前，强制在服务端校验来源群与操作者权限
func handleApprovalCallback(auth *pendingAuth, approved bool, cbCtx *context.CallbackContext) error {
	stateMu.RLock()
	cfg := config
	stateMu.RUnlock()

	// 1. 验证群聊来源
	if cbCtx.GroupId == "" || cbCtx.GroupId != cfg.GroupID {
		_ = cbCtx.Done()
		_ = cbCtx.Text("❌ 无权操作：非法来源群聊").Send()
		return nil
	}

	// 2. 服务端强校验操作者权限
	if len(cfg.AllowedUsers) > 0 {
		if !slices.Contains(cfg.AllowedUsers, cbCtx.UserId) {
			_ = cbCtx.Done()
			_ = cbCtx.Text("❌ 无权操作：您不在 SSH 审批授权名单中").Send()
			return nil
		}
	} else {
		// 默认且仅限群管理员或群主
		if !constant.RoleAdmin.CanUse(cbCtx.Role) {
			_ = cbCtx.Done()
			_ = cbCtx.Text("❌ 无权操作：仅群管理员或群主可以审批 SSH 登录").Send()
			return nil
		}
	}

	return auth.resolve(approved, cbCtx)
}

func (a *pendingAuth) resolve(approved bool, cbCtx *context.CallbackContext) error {
	// 必须首先通过 CAS 原子抢占处理权
	if !a.resolved.CompareAndSwap(false, true) {
		if cbCtx != nil {
			_ = cbCtx.Done()
			_ = cbCtx.Text(fmt.Sprintf("⚠️ 请求 %s 已处理或已失效，请勿重复操作", a.req.ID)).Send()
		}
		return nil
	}

	// 抢占成功后统一执行 cleanup（停止 timer，移出 map，归还待审批计数）
	a.cleanup()

	if cbCtx != nil {
		_ = cbCtx.Done()
	}

	resp := BotIPCResponse{
		Version:  "1",
		Type:     "approve",
		ID:       a.req.ID,
		Approval: approved,
	}

	// 先向 IPC 发送，并根据返回结果决定反馈与后续清理
	var sendErr error
	if a.conn != nil && a.conn.active.Load() {
		sendErr = a.conn.SendResponse(resp)
	} else {
		sendErr = fmt.Errorf("IPC 连接已断开")
	}

	actionName := "批准"
	if !approved {
		actionName = "拒绝"
	}

	if sendErr != nil {
		a.status.Store(int32(StatusDeliveryFailed))
		log.Printf("[sshauth] ❌ 审批响应向 IPC 发送失败 %s (标记为交付失败并撤回审批消息): %v", a.req.ID, sendErr)

		// 发送失败时明确撤回群消息，避免残留按钮继续误导群用户
		a.mu.Lock()
		msg := a.msg
		a.mu.Unlock()
		if msg != nil && msg.MsgId != "" {
			if err := msg.DeleteMessage(); err != nil {
				log.Printf("[sshauth] failed to recall message %s after IPC delivery failure for request %s: %v", msg.MsgId, a.req.ID, err)
			} else {
				log.Printf("[sshauth] successfully recalled message %s after IPC delivery failure for request %s", msg.MsgId, a.req.ID)
			}
		}

		if cbCtx != nil {
			_ = cbCtx.Text(fmt.Sprintf("❌ 审批%s失败：向 SSH 认证服务交付响应失败 (%v)。该审批已作废，原审批消息已撤回。", actionName, sendErr)).Send()
		}
		return sendErr
	}

	a.status.Store(int32(StatusDelivered))
	log.Printf("[sshauth] 📤 审批响应已发送至 IPC %s: approval=%v (本地已写入，最终生效取决于 ssh-qqauth 状态)", a.req.ID, approved)
	if cbCtx != nil {
		_ = cbCtx.Text(fmt.Sprintf("✅ 审批响应已发送：%s来自 %s 的 %s 登录申请 (ID: %s)。", actionName, a.req.RemoteHost, a.req.User, a.req.ID)).Send()
	}

	return nil
}

func (a *pendingAuth) onTimeout() {
	if !a.resolved.CompareAndSwap(false, true) {
		return // 已经被 resolve 或 invalidate 抢占
	}

	a.status.Store(int32(StatusTimedOut))
	a.cleanup()

	stateMu.RLock()
	timeoutSec := config.TimeoutSeconds
	stateMu.RUnlock()

	log.Printf("[sshauth] Request %s timed out after %d seconds. Recalling message (not passing to IPC)...", a.req.ID, timeoutSec)

	// 超时则撤回群消息，不向 IPC 传递
	a.mu.Lock()
	msg := a.msg
	a.mu.Unlock()
	if msg != nil && msg.MsgId != "" {
		if err := msg.DeleteMessage(); err != nil {
			log.Printf("[sshauth] failed to recall timed-out message %s for request %s: %v", msg.MsgId, a.req.ID, err)
		} else {
			log.Printf("[sshauth] successfully recalled timed-out message %s for request %s", msg.MsgId, a.req.ID)
		}
	}
}

// invalidate 在连接断开、服务重启或发送失败时调用，使请求失效并撤回消息
func (a *pendingAuth) invalidate(reason string) {
	if !a.resolved.CompareAndSwap(false, true) {
		return
	}

	a.status.Store(int32(StatusInvalidated))
	a.cleanup()

	log.Printf("[sshauth] Request %s invalidated (%s). Recalling message...", a.req.ID, reason)

	a.mu.Lock()
	msg := a.msg
	a.mu.Unlock()
	if msg != nil && msg.MsgId != "" {
		_ = msg.DeleteMessage()
	}
}

func statusCommand(ctx *context.MessageContext) error {
	stateMu.RLock()
	cfg := config
	hasClient := (client != nil)
	stateMu.RUnlock()

	workerMu.Lock()
	isConnected := (currentConn != nil && currentConn.active.Load())
	workerMu.Unlock()

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

	statusText := "已连接"
	if !cfg.Enabled {
		statusText = "已禁用"
	} else if !hasClient {
		statusText = "未就绪 (等待 QQ Client)"
	} else if !isConnected {
		statusText = "连接断开 (正在尝试重连)"
	}

	text := fmt.Sprintf("### 🛡️ SSH 登录认证插件状态\n\n"+
		"- **状态**: %s\n"+
		"- **授权群 OpenID**: `%s`\n"+
		"- **IPC 地址**: `%s` (unix / connect)\n"+
		"- **超时时间**: %d 秒\n"+
		"- **待审批请求数**: %d%s",
		statusText, cfg.GroupID, cfg.SocketPath,
		cfg.TimeoutSeconds, pendingCount, pendingInfo.String())

	return ctx.Markdown(text).Send()
}
