package message

import (
	"Plrx/lib/assets"
	"Plrx/lib/templates"
	"fmt"
	"strings"
)

// ImageMessage 图片部件。
type ImageMessage struct {
	*Message
	Src     any    `json:"-"` // string(路径/data/base64/URL) 或 []byte
	Summary string `json:"-"`
	// ForceMarkdown 由构造器按上下文设置：true 强制内嵌 markdown。
	ForceMarkdown bool `json:"-"`
	// ForceMedia 由构造器在降级路径设置：true 跳过 markdown 直接走 QQ 媒体。
	ForceMedia bool `json:"-"`
}

func (*ImageMessage) part() {}

// Send 发送：markdown 内嵌优先，失败降级独立媒体。
func (msg *ImageMessage) Send() error {
	if !msg.ForceMedia {
		useMD := msg.ForceMarkdown || (msg.Qapi != nil && msg.Qapi.GlobalMarkdown)
		if useMD && msg.Qapi != nil {
			if frag, err := msg.Fragment(msg.Qapi.Assets); err == nil && frag != "" {
				md := &MarkdownMessage{Message: msg.Message, Markdown: templates.Markdown{Content: frag}}
				md.Init()
				return md.Send()
			}
		}
	}
	return msg.sendMedia()
}

// Fragment 解析为 markdown 图片语法；公网 URL 直通，其余走图床。
func (msg *ImageMessage) Fragment(host *assets.ImageHost) (string, error) {
	summary := msg.Summary
	if summary == "" {
		summary = "图片"
	}
	if s, ok := msg.Src.(string); ok {
		if host == nil || host.Size() == 0 {
			if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
				return fmt.Sprintf("![%s](%s)", summary, s), nil
			}
			return "", fmt.Errorf("图床未配置且图片非公网 URL")
		}
	}
	if host == nil || host.Size() == 0 {
		return "", fmt.Errorf("图床未配置且图片非公网 URL")
	}
	resolved, err := host.Resolve(msg.Src)
	if err != nil || resolved.URL == "" {
		return "", fmt.Errorf("图床转换失败: %v", err)
	}
	if resolved.Width > 0 && resolved.Height > 0 {
		return fmt.Sprintf("![%s #%dpx #%dpx](%s)", summary, resolved.Width, resolved.Height, resolved.URL), nil
	}
	return fmt.Sprintf("![%s](%s)", summary, resolved.URL), nil
}

// sendMedia 上传 QQ 后以媒体消息发送。
func (msg *ImageMessage) sendMedia() error {
	up := MediaUploadFor(1, msg.Src, msg.Summary)
	fileInfo, err := msg.Qapi.UploadMedia(msg.Target, msg.GroupId, msg.UserId, up)
	if err != nil {
		return err
	}
	media := &MediaMessage{Message: msg.Message, Media: MediaContent{FileInfo: fileInfo}}
	media.Init()
	return media.Send()
}
