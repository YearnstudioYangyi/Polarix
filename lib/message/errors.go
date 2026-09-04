package message

import (
	"Plrx/lib/constant"
	"fmt"
)

// 消息对象已经被使用
type MessageUsed struct {
	MessageId string
}

func (m *MessageUsed) Error() string {
	return fmt.Sprintf("Reply message for %v was used, please generate a new one", m.MessageId)
}

// JSON格式化失败
type JSONMarshalError struct {
	Err error
}

func (e *JSONMarshalError) Error() string {
	return fmt.Sprintf("Failed when marshal: %v", e.Err)
}

func (e *JSONMarshalError) Unwarp() error {
	return e.Err
}

// 回复消息超过上限
type ReplyMessageReachLimit struct{}

func (e *ReplyMessageReachLimit) Error() string {
	return "Reply message count reached limit: 5"
}

// 未知的消息目的类型
type UnknownMessageTarget struct {
	Target constant.MessageOrigin
}

func (e *UnknownMessageTarget) Error() string {
	return fmt.Sprintf("Except message target: group(0)/private(1), received: %v", e.Target)
}
