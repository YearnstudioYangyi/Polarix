package email

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// ContentType 邮件内容类型
type ContentType string

const (
	ContentTypePlain ContentType = "text/plain; charset=UTF-8"
	ContentTypeHTML  ContentType = "text/html; charset=UTF-8"
)

// SMTPClient 邮件客户端结构体
type SMTPClient struct {
	Host     string
	Port     int
	Username string
	Password string
	UseSSL   bool // 是否直接开启 TLS/SSL (如 465 端口)
}

// NewSMTPClient 初始化 SMTP 实例
func NewSMTPClient(host string, port int, username, password string, useSSL bool) *SMTPClient {
	return &SMTPClient{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		UseSSL:   useSSL,
	}
}

// SendMail 发送邮件方法
// subject: 邮件主题
// body: 邮件正文内容 (纯文本或 HTML 字符串)
// cType: 内容类型 (ContentTypePlain 或 ContentTypeHTML)
// to: 接收方邮箱列表
func (c *SMTPClient) SendMail(subject, body string, cType ContentType, to []string) error {
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	auth := smtp.PlainAuth("", c.Username, c.Password, c.Host)

	// 构建 RFC 822 标准格式邮件报文
	headers := make(map[string]string)
	headers["From"] = c.Username
	headers["To"] = strings.Join(to, ", ")
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = string(cType)

	var message strings.Builder
	for k, v := range headers {
		message.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	message.WriteString("\r\n" + body)

	msgBytes := []byte(message.String())

	// SSL/TLS (如 465 端口) 发送逻辑
	if c.UseSSL {
		tlsConfig := &tls.Config{
			ServerName: c.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("tls dial error: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, c.Host)
		if err != nil {
			return fmt.Errorf("create smtp client error: %w", err)
		}
		defer client.Quit()

		if auth != nil {
			if ok, _ := client.Extension("AUTH"); ok {
				if err = client.Auth(auth); err != nil {
					return fmt.Errorf("auth error: %w", err)
				}
			}
		}

		if err = client.Mail(c.Username); err != nil {
			return fmt.Errorf("mail from error: %w", err)
		}

		for _, addr := range to {
			if err = client.Rcpt(addr); err != nil {
				return fmt.Errorf("rcpt to error: %w", err)
			}
		}

		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("data error: %w", err)
		}

		if _, err = w.Write(msgBytes); err != nil {
			return fmt.Errorf("write message error: %w", err)
		}

		return w.Close()
	}

	// 普通连接 / STARTTLS (如 25, 587 端口)
	return smtp.SendMail(addr, auth, c.Username, to, msgBytes)
}
