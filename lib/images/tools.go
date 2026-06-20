package images

import (
	"fmt"
	"image"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
)

// GetImageDimensions 请求 URL 获取图片并返回宽高
func GetImageDimensions(url string) (int, int, error) {
	// 发起HTTP GET请求
	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	// 使用DecodeConfig读取图片配置
	config, _, err := image.DecodeConfig(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}
