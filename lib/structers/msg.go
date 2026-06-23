package structers

import (
	"Plrx/lib/structers/buttons"
	"encoding/json"
)

// 消息类型枚举
type MessageType uint8

const (
	PlainText MessageType = 0
	Markdown  MessageType = 2
)

// 来源枚举
type MessageFrom uint8

const (
	PrivateMessage MessageFrom = iota
	GroupMessage
)

type Message struct {
	Content string
	MessageType
	MessageFrom
	UserId    string
	UnionId   string
	GroupId   string
	MessageId string // 存在的时候即采用被动回复策略
	Keyboard  buttons.Keyboard
}

func (msg *Message) GenerateJSON() []byte {
	keyboard, err := buttons.GenerateJson(msg.Keyboard)
	if err != nil {
		return make([]byte, 0)
	}
	switch msg.MessageType {
	case PlainText:
		type MsgData struct {
			Content   string          `json:"content"`
			MsgType   uint8           `json:"msg_type"`
			MessageID string          `json:"msg_id,omitempty"`
			Keyboard  json.RawMessage `json:"keyboard,omitempty"`
		}
		data := MsgData{Content: msg.Content, MessageID: msg.MessageId, MsgType: uint8(msg.MessageType), Keyboard: json.RawMessage(keyboard)}
		json, _ := json.Marshal(data)
		return json
	case Markdown:
		type MsgData struct {
			MsgType   uint8  `json:"msg_type"`
			MessageID string `json:"msg_id,omitempty"`
			Markdown  struct {
				Content string `json:"content"`
			} `json:"markdown"`
			Keyboard json.RawMessage `json:"keyboard,omitempty"`
		}
		data := MsgData{MsgType: uint8(msg.MessageType), MessageID: msg.MessageId, Markdown: struct {
			Content string `json:"content"`
		}{
			Content: msg.Content,
		},
			Keyboard: json.RawMessage(keyboard),
		}
		json, _ := json.Marshal(data)
		return json
	default:
		return []byte{}
	}
}
