package assets

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"Plrx/lib/images"
	"Plrx/lib/requests"
)

var imgRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// NewMultipart 便捷构造 multipart 请求体。
func NewMultipart() *requests.Multipart { return requests.NewMultipart() }

// Client 图床 HTTP 客户端，包装 requests.Client。
type Client struct {
	*requests.Client
}

// NewClient 创建图床专用客户端。
func NewClient(timeout int) *Client {
	return &Client{Client: requests.Init(timeout)}
}

// ImageHost 图床聚合器：按配置顺序（优先级从高到低）依次尝试，失败自动切换下一个。
type ImageHost struct {
	providers []ImageProvider
	whitelist []string
	uploaded  int64
}

// HostConfig 图床配置，对应独立 assets.json（不入 git）。
type HostConfig struct {
	Providers []ProviderItem `json:"providers"`
	Whitelist []string       `json:"whitelist"`
}

// ProviderItem 单个图床 provider 配置项。Priority 越大越优先，同级保持配置顺序。
type ProviderItem struct {
	Name     string         `json:"name"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config,omitempty"`
}

// NewHost 从配置创建图床聚合器：过滤未启用的 provider，
// 按 Priority 降序排列（稳定排序，同级保持配置顺序）。
func NewHost(cfg HostConfig, cl *Client) *ImageHost {
	h := &ImageHost{whitelist: cfg.Whitelist}
	enabled := make([]ProviderItem, 0, len(cfg.Providers))
	for _, item := range cfg.Providers {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		enabled = append(enabled, item)
	}
	slices.SortStableFunc(enabled, func(a, b ProviderItem) int {
		return cmp.Compare(b.Priority, a.Priority)
	})
	for _, item := range enabled {
		p, err := Instantiate(item.Name, cl, item.Config)
		if err != nil {
			continue
		}
		h.providers = append(h.providers, p)
	}
	return h
}

// newHostWithProviders 测试用：直接注入 provider。
func newHostWithProviders(providers []ImageProvider, whitelist []string) *ImageHost {
	return &ImageHost{providers: providers, whitelist: whitelist}
}

// Size 已配置的 provider 数量（0 表示未启用图床）。
func (h *ImageHost) Size() int { return len(h.providers) }

// Stats 返回本次进程累计上传数。
func (h *ImageHost) Stats() int64 { return h.uploaded }

// ImgToURL 把图片 src 转成公网 URL。
// 公网 URL 直通；命中白名单原样返回；本地/内网/base64 上传图床。
func (h *ImageHost) ImgToURL(src string) (ResolvedImage, error) {
	for _, prefix := range h.whitelist {
		if strings.HasPrefix(src, prefix) {
			return ResolvedImage{URL: src}, nil
		}
	}

	if !isLocal(src) {
		return ResolvedImage{URL: src}, nil
	}

	input, err := h.load(src)
	if err != nil {
		return ResolvedImage{}, fmt.Errorf("load image: %w", err)
	}

	url, err := h.upload(input)
	if err != nil {
		return ResolvedImage{}, err
	}

	width, height := 0, 0
	if sz := images.Probe(input.Buffer); sz != nil {
		width, height = sz.Width, sz.Height
	}

	return ResolvedImage{URL: url, Width: width, Height: height}, nil
}

// ProcessMarkdown 处理 markdown 中的图片引用：local/base64 上传图床，替换为
// ![alt #wpx #hpx](公网URL)。公网 URL / 白名单命中时原样保留（含已带尺寸标注），
// 避免二次处理丢失信息；图床上传失败时保持原样。
func (h *ImageHost) ProcessMarkdown(input string) string {
	if h == nil || len(h.providers) == 0 {
		return input
	}
	return imgRe.ReplaceAllStringFunc(input, func(match string) string {
		m := imgRe.FindStringSubmatch(match)
		if len(m) < 3 {
			return match
		}
		alt, src := m[1], m[2]
		resolved, err := h.ImgToURL(src)
		if err != nil {
			return match
		}
		// 公网 URL / 白名单直通：URL 未变则原样返回，保留 alt 中已有的尺寸标注
		if resolved.URL == src {
			return match
		}
		if resolved.Width > 0 && resolved.Height > 0 {
			return fmt.Sprintf("![%s #%dpx #%dpx](%s)", alt, resolved.Width, resolved.Height, resolved.URL)
		}
		return fmt.Sprintf("![%s](%s)", alt, resolved.URL)
	})
}

// isLocal 判断 src 是否需要上传：本地文件、内网、base64 data URL 为 true；公网 URL 为 false。
func isLocal(src string) bool {
	if strings.HasPrefix(src, "data:") || strings.HasPrefix(src, "file://") {
		return true
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		u, err := url.Parse(src)
		if err != nil {
			return true
		}
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}
		if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "172.") || strings.HasPrefix(host, "192.168.") {
			return true
		}
		return false
	}
	return true
}

// load 加载本地/内网/base64 资源为上传输入。
func (h *ImageHost) load(src string) (ProviderInput, error) {
	if strings.HasPrefix(src, "data:") {
		comma := strings.IndexByte(src, ',')
		if comma < 0 || comma >= len(src)-1 {
			return ProviderInput{}, fmt.Errorf("invalid data URL")
		}
		mimePart := src[5:comma]
		raw, err := base64.StdEncoding.DecodeString(src[comma+1:])
		if err != nil {
			return ProviderInput{}, fmt.Errorf("base64 decode: %w", err)
		}
		mimeType := strings.SplitN(mimePart, ";", 2)[0]
		return ProviderInput{Buffer: raw, Filename: "inline", MimeType: mimeType}, nil
	}

	src = strings.TrimPrefix(src, "file://")
	data, err := os.ReadFile(src)
	if err != nil {
		return ProviderInput{}, fmt.Errorf("read file: %w", err)
	}
	return ProviderInput{Buffer: data, Filename: path.Base(src)}, nil
}

// upload 按优先级依次尝试 provider，收集错误，全失败时汇总抛出。
func (h *ImageHost) upload(input ProviderInput) (string, error) {
	var errs []error
	ctx := context.Background()
	for _, p := range h.providers {
		url, err := p.Upload(ctx, input)
		if err == nil {
			h.uploaded++
			return url, nil
		}
		errs = append(errs, fmt.Errorf("[%s] %w", p.Name(), err))
	}
	return "", fmt.Errorf("all providers failed: %w", errors.Join(errs...))
}
