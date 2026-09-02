package images

import (
	"fmt"
	"image"
	"io"
	"net/http"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// dimClient 探测尺寸专用客户端：限定超时与响应体上限，防慢速/超大响应拖垮协程。
var dimClient = &http.Client{
	Timeout: 5 * time.Second,
}

const maxProbeBytes = 1 << 20 // 探测尺寸只需头部，1MB 足够

// GetImageDimensions 请求 URL 获取图片并返回宽高。
func GetImageDimensions(url string) (int, int, error) {
	resp, err := dimClient.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	config, _, err := image.DecodeConfig(io.LimitReader(resp.Body, maxProbeBytes))
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}
