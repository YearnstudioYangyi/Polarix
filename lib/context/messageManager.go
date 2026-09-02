package context

import (
	"Plrx/lib/constant"
	"Plrx/lib/message"
	"Plrx/lib/qqapi"
	"Plrx/lib/templates"
)

type MessageManager struct {
	MessageId string
	EventId   string
	GroupId   string
	UserId    string
	Target    constant.MessageOrigin
	Qapi      *qqapi.Client
	ref       *message.MsgRef
}

// 生成包含元信息的消息结构
func (manager *MessageManager) baseStruct() *message.Message {
	msg := &message.Message{
		EventId: manager.EventId,
		MsgId:   manager.MessageId,
		Qapi:    manager.Qapi,
		GroupId: manager.GroupId,
		UserId:  manager.UserId,
		Target:  manager.Target,
	}
	// 确保引用的是同一个计数器
	if manager.ref == nil {
		msg.InitRef()
		manager.ref = msg.MsgRef
	} else {
		msg.MsgRef = manager.ref
	}
	return msg
}

// Text 生成纯文本回复消息；全局 markdown 开启时自动转 markdown。
func (manager *MessageManager) Text(content string) *message.TextMessage {
	metamsg := manager.baseStruct()
	msg := message.TextMessage{
		Message: metamsg,
	}
	msg.Type = constant.PlainText
	msg.Content(content)
	msg.Init()
	if manager.Qapi != nil && manager.Qapi.GlobalMarkdown {
		msg.MarkdownMode = true
	}
	return &msg
}

// Markdown 生成 Markdown 回复消息。
func (manager *MessageManager) Markdown(content string) *message.MarkdownMessage {
	metamsg := manager.baseStruct()
	msg := message.MarkdownMessage{
		Message: metamsg,
	}
	msg.Init()
	msg.Type = constant.Markdown
	msg.Markdown = templates.Markdown{
		Content: content,
	}
	if manager.Qapi != nil {
		msg.SetAssets(manager.Qapi.Assets)
	}
	return &msg
}

// Media creates a rich-media message from file_info returned by QQ's upload API.
func (manager *MessageManager) Media(fileInfo string) *message.MediaMessage {
	msg := &message.MediaMessage{
		Message: manager.baseStruct(),
		Media: message.MediaContent{
			FileInfo: fileInfo,
		},
	}
	msg.Type = constant.Media
	msg.Init()
	return msg
}

// MarkdownTemplate 填充 Markdown 模板并构造消息。
func (manager *MessageManager) MarkdownTemplate(id string, args *templates.Args) (*message.MarkdownMessage, error) {
	var content string
	var err error
	if args == nil {
		content, err = templates.FillMarkdownTemplate(id, templates.Args{})
	} else {
		content, err = templates.FillMarkdownTemplate(id, *args)
	}
	if err != nil {
		return nil, err
	}
	return manager.Markdown(content), nil
}

// UnsafeMarkdownTemplate 填充失败时 panic。
func (manager *MessageManager) UnsafeMarkdownTemplate(id string, args *templates.Args) *message.MarkdownMessage {
	content, err := templates.FillMarkdownTemplate(id, *args)
	if err != nil {
		panic(err)
	}
	return manager.Markdown(content)
}
