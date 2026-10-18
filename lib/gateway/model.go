package gateway

import "encoding/json"

// GatewayMessage 表示网关传输的通用消息结构
type GatewayMessage struct {
	ID string          `json:"id,omitempty"`
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  int64           `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// HelloData 表示 OpCode 10 Hello 消息体
type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// IdentifyData 表示 OpCode 2 Identify 鉴权消息体
type IdentifyData struct {
	Token      string   `json:"token"`
	Intents    int      `json:"intents"`
	Shard      []uint32 `json:"shard,omitempty"`
	Properties struct {
		OS      string `json:"$os,omitempty"`
		Browser string `json:"$browser,omitempty"`
		Device  string `json:"$device,omitempty"`
	} `json:"properties,omitempty"`
}

// ResumeData 表示 OpCode 6 Resume 恢复连接消息体
type ResumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

// HeartbeatMessage 表示 OpCode 1 心跳消息体
type HeartbeatMessage struct {
	Op int    `json:"op"`
	D  *int64 `json:"d"`
}

// ReadyData 表示 OpCode 0 READY 事件消息体
type ReadyData struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"user"`
	Shard [2]int `json:"shard"`
}
