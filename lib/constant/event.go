package constant

type EventType string

const (
	INTERACTION_CREATE      EventType = "INTERACTION_CREATE"
	C2C_MESSAGE_CREATE      EventType = "C2C_MESSAGE_CREATE"
	GROUP_AT_MESSAGE_CREATE EventType = "GROUP_AT_MESSAGE_CREATE"
	GROUP_MESSAGE_CREATE    EventType = "GROUP_MESSAGE_CREATE"
	GROUP_JOIN_REQUEST      EventType = "GROUP_JOIN_REQUEST"
	GROUP_MEMBER_ADD        EventType = "GROUP_MEMBER_ADD"
)

// 网关 OpCode 常量
const (
	OpCodeDispatch           = 0  // 服务端进行消息推送 (webhook/websocket)
	OpCodeHeartbeat          = 1  // 客户端或服务端发送心跳
	OpCodeIdentify           = 2  // 客户端发送鉴权
	OpCodeResume             = 6  // 客户端恢复连接
	OpCodeReconnect          = 7  // 服务端通知客户端重新连接
	OpCodeInvalidSession     = 9  // 当 identify 或 resume 的时候，如果参数有错，服务端会返回该消息
	OpCodeHello              = 10 // 当客户端与网关建立 ws 连接之后，网关下发的第一条消息
	OpCodeHeartbeatACK       = 11 // 当发送心跳成功之后，收到该消息
	OpCodeHTTPCallbackACK    = 12 // 仅用于 http 回调模式的回包
	OpCodeCallbackValidation = 13 // 开放平台对机器人服务端进行验证
)

// 事件订阅 Intents 标记位
const (
	IntentGuilds                = 1 << 0  // 频道基础事件
	IntentGuildMembers          = 1 << 1  // 频道成员事件
	IntentGuildMessages         = 1 << 9  // 频道消息事件 (仅私域)
	IntentGuildMessageReactions = 1 << 10 // 频道消息表情表态
	IntentDirectMessage         = 1 << 12 // 私信事件
	IntentGroupAndC2C           = 1 << 25 // 群聊及C2C单聊事件
	IntentInteraction           = 1 << 26 // 互动事件
	IntentMessageAudit          = 1 << 27 // 消息审核
	IntentForums                = 1 << 28 // 论坛事件 (仅私域)
	IntentAudioAction           = 1 << 29 // 音频事件
	IntentPublicGuildMessages   = 1 << 30 // 公域频道消息事件
)

func IsValidEventType(s string) bool {
	switch EventType(s) {
	case INTERACTION_CREATE, C2C_MESSAGE_CREATE, GROUP_AT_MESSAGE_CREATE, GROUP_MESSAGE_CREATE, GROUP_JOIN_REQUEST, GROUP_MEMBER_ADD:
		return true
	default:
		return false
	}
}
