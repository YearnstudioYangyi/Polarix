package message

import (
	"encoding/json"
)

// AtMessage 艾特部件：All 为 true 时艾特所有人。
type AtMessage struct {
	*Message
	OpenID string `json:"-"`
	All    bool   `json:"-"`
}

func (*AtMessage) part() {}

func (msg *AtMessage) Marshal() ([]byte, error) {
	content := "@everyone"
	if !msg.All {
		content = "<@" + msg.OpenID + ">"
	}
	type txt struct {
		*Message
		Content string `json:"content"`
	}
	return json.Marshal(txt{
		Message: msg.Message,
		Content: content,
	})
}
