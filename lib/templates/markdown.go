package templates

import (
	"botOffical/lib/images"
	"fmt"
	"regexp"
	"strings"
)

type MarkdownTemplate struct {
	Id       string
	Template string
	args     []string
}

var MarkdownTemplates []*MarkdownTemplate

func processTemplate(input string) (string, []string) {
	// 匹配 {{ 任意内容 }}，使用非贪婪匹配
	re := regexp.MustCompile(`\{\{(.*?)\}\}`)

	var args []string
	seen := make(map[string]bool) // 用于判断参数是否重复

	// 动态替换并收集参数
	result := re.ReplaceAllStringFunc(input, func(match string) string {
		// 提取出参数并去掉首尾空格
		content := match[2 : len(match)-2]
		trimmed := strings.TrimSpace(content)

		// 如果参数不为空且之前没收集过，则加入 args 列表
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			args = append(args, trimmed)
		}

		// 返回替换后的标准格式
		return "{{" + trimmed + "}}"
	})

	return result, args
}

func NewMarkdownTemplate(Id string, Template string) {
	template, args := processTemplate(Template)
	MarkdownTemplates = append(MarkdownTemplates, &MarkdownTemplate{
		Id:       Id,
		Template: template,
		args:     args,
	})
}

func IsMarkdownTemplateExit(Id string) bool {
	for _, v := range MarkdownTemplates {
		if v.Id == Id {
			return true
		}
	}
	return false
}

// 适配QQ的图片显示
func ProcessMarkdownImages(input string) string {
	re := regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)

	return re.ReplaceAllStringFunc(input, func(match string) string {
		submatch := re.FindStringSubmatch(match)
		alt := submatch[1]
		url := submatch[2]

		// 获取图片尺寸
		width, height, err := images.GetImageDimensions(url)
		if err != nil {
			fmt.Printf("获取图片失败 [%s]: %v\n", url, err)
			return match // 失败时保持原样
		}

		// 格式化输出
		return fmt.Sprintf("![%s #%dpx #%dpx](%s)", alt, width, height, url)
	})
}

func FillMarkdownTemplate(Id string, args map[string]string) (string, error) {
	for _, v := range MarkdownTemplates {
		if v.Id == Id {
			template := v.Template
			for key, value := range args {
				template = strings.ReplaceAll(template, fmt.Sprintf("{{%v}}", key), value)
			}
			_, afterDo := processTemplate(template)
			if len(afterDo) > 0 {
				var lostArgs string = afterDo[0]
				for k, i := range afterDo {
					if k == 0 {
						continue
					}
					lostArgs += fmt.Sprintf(", %v", i)
				}
				return "", fmt.Errorf("Lost args: %v", lostArgs)
			} else {
				return template, nil
			}
		}
	}
	return "", fmt.Errorf("Template %v not found", Id)
}
