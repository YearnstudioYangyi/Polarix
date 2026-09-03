package context

import (
	"Plrx/lib/contract"
	"Plrx/lib/message"
	"errors"
	"strings"
	"sync"
)

// MsgBuilder 消息聚合器：Add 实体部件，Send 时分拣合并发送。
type MsgBuilder struct {
	m     *MessageManager
	parts []message.Part
	kb    contract.CanMarshal
	quote string
}

// Msg 开启一条消息聚合。
func (manager *MessageManager) Msg() *MsgBuilder {
	return &MsgBuilder{m: manager}
}

// Add 追加消息部件（ctx.Text/Markdown/Image 等构造的实体）。
func (b *MsgBuilder) Add(parts ...message.Part) *MsgBuilder {
	b.parts = append(b.parts, parts...)
	return b
}

// Keyboard 挂按钮板，仅随 markdown 消息发送。
func (b *MsgBuilder) Keyboard(kb contract.CanMarshal) *MsgBuilder {
	b.kb = kb
	return b
}

// Quote 引用回复指定消息。
func (b *MsgBuilder) Quote(messageID string) *MsgBuilder {
	b.quote = messageID
	return b
}

// Send 分拣发送：文字/艾特/图床图/markdown 合成一条主消息，
// 语音视频文件与降级图拆独立媒体；共享同一 seq 计数串联。
func (b *MsgBuilder) Send() error {
	if len(b.parts) == 0 && b.kb == nil {
		return nil
	}
	out := b.plan()
	var errs []error
	for _, p := range out {
		if err := p.Send(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// plan 分拣，返回有序发送列表（主消息在前，独立媒体在后）。
func (b *MsgBuilder) plan() []Sender {
	host := b.m.Qapi.Assets
	globalMD := b.m.Qapi != nil && b.m.Qapi.GlobalMarkdown

	// 主消息是否 markdown：按钮/markdown 段强制；全局 markdown 且含文本或图时转 markdown。
	useMarkdown := b.kb != nil
	hasText, hasImage := false, false
	for _, p := range b.parts {
		switch p.(type) {
		case *message.MarkdownMessage:
			useMarkdown = true
		case *message.TextMessage:
			hasText = true
		case *message.ImageMessage:
			hasImage = true
		}
	}
	if globalMD && (hasText || hasImage) {
		useMarkdown = true
	}

	// 图片槽位：仅 markdown 模式才需图床并发解析；否则直接走 QQ 媒体上传。
	var imgSlots []imgResult
	var imgPos []int
	if useMarkdown {
		for i, p := range b.parts {
			if _, ok := p.(*message.ImageMessage); ok {
				imgPos = append(imgPos, i)
				imgSlots = append(imgSlots, imgResult{})
			}
		}
		if len(imgSlots) > 0 {
			var wg sync.WaitGroup
			wg.Add(len(imgSlots))
			for n, i := range imgPos {
				im := b.parts[i].(*message.ImageMessage)
				go func(n int, im *message.ImageMessage) {
					defer wg.Done()
					imgSlots[n].frag, imgSlots[n].err = im.Fragment(host)
				}(n, im)
			}
			wg.Wait()
		}
	}

	var inline strings.Builder
	var media []Sender

	next := 0
	for _, p := range b.parts {
		switch v := p.(type) {
		case *message.TextMessage:
			inline.WriteString(v.TextContent)
		case *message.AtMessage:
			if v.All {
				if useMarkdown {
					inline.WriteString("<qqbot-at-everyone />")
				} else {
					inline.WriteString("@everyone")
				}
			} else {
				inline.WriteString("<@")
				inline.WriteString(v.OpenID)
				inline.WriteString(">")
			}
		case *message.MarkdownMessage:
			inline.WriteString(v.Markdown.Content)
		case *message.ImageMessage:
			if useMarkdown {
				r := imgSlots[next]
				next++
				if r.err == nil && r.frag != "" {
					inline.WriteByte('\n')
					inline.WriteString(r.frag)
					inline.WriteByte('\n')
					continue
				}
				// 图床失败降级：强制直接走 QQ 媒体，不再重试 Fragment
				v.ForceMarkdown = false
				v.ForceMedia = true
			}
			media = append(media, v)
		case *message.UploadMessage:
			media = append(media, v)
		case *message.MediaMessage:
			media = append(media, v)
		}
	}

	out := make([]Sender, 0, 1+len(media))
	switch {
	case useMarkdown && (inline.Len() > 0 || b.kb != nil):
		content := inline.String()
		if strings.TrimSpace(content) == "" {
			content = " "
		}
		mdMsg := b.m.Markdown(content)
		if b.kb != nil {
			mdMsg.Keyboard(b.kb)
		}
		if b.quote != "" {
			mdMsg.QuoteTo(b.quote)
		}
		out = append(out, mdMsg)
	case inline.Len() > 0:
		msg := b.m.Text(inline.String())
		if b.quote != "" {
			msg.QuoteTo(b.quote)
		}
		out = append(out, msg)
	}
	out = append(out, media...)
	return out
}

// imgResult 并发解析的图片结果，槽位保序。
type imgResult struct {
	frag string
	err  error
}
