package context

import (
	"Plrx/lib/constant"
	"Plrx/lib/message"
	"Plrx/lib/qqapi"
	"Plrx/lib/templates"
	"fmt"
	"os"
	"path"
	"strings"
)

// Sender 可发送的消息对象（Text/Markdown/Media 均实现）。
type Sender interface {
	Send() error
}

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

// Image 生成图片消息（框架层统一处理，插件只插图片）。
// 图片一律先经图床转公网 URL（白名单除外）；图床未配置或全失败时降级：
// 公网 URL 直接交 QQ 媒体上传，本地文件读字节后上传。
//
// global_markdown 开启时图片内嵌 markdown（![alt #wpx #hpx](图床URL)），
// 对应参考适配器的 markdown 上下文路径。否则以媒体消息发送，对应非 markdown 路径。
func (manager *MessageManager) Image(src string) (Sender, error) {
	url, width, height := src, 0, 0
	if host := manager.Qapi.Assets; host != nil && host.Size() > 0 {
		if resolved, err := host.ImgToURL(src); err == nil {
			url, width, height = resolved.URL, resolved.Width, resolved.Height
		}
	}

	if manager.Qapi != nil && manager.Qapi.GlobalMarkdown {
		md := "![图片"
		if width > 0 && height > 0 {
			md += fmt.Sprintf(" #%dpx #%dpx", width, height)
		}
		md += fmt.Sprintf("](%s)", url)
		return manager.Markdown(md), nil
	}

	up := qqapi.MediaUpload{FileType: 1}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		up.URL = url
	} else {
		data, err := os.ReadFile(strings.TrimPrefix(url, "file://"))
		if err != nil {
			return nil, err
		}
		up.Data = data
		up.Filename = path.Base(url)
	}
	fileInfo, err := manager.Qapi.UploadMedia(manager.Target, manager.GroupId, manager.UserId, up)
	if err != nil {
		return nil, err
	}
	return manager.Media(fileInfo), nil
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
