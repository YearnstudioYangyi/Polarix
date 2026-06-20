package qqapi

import (
	"botOffical/lib/structers"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Client struct {
	ProxyAPI    string
	AppID       string
	AppSecret   string
	Request     *http.Client
	accessToken string
	expireAt    time.Time
	lock        sync.RWMutex
}

func Init(AppID string, AppSecret string, ProxyAPI string) Client {
	return Client{
		AppID:     AppID,
		AppSecret: AppSecret,
		ProxyAPI:  ProxyAPI,
		Request: &http.Client{
			Timeout: 5 * time.Second,
		},
		accessToken: "",
	}
}

// 生成Access Token
func (c *Client) GetAccessToken() (string, error) {
	// log.Print("正在生成AccessToken")
	c.lock.RLock()
	if c.accessToken != "" && time.Now().Before(c.expireAt) {
		// 有效
		// log.Print("已有AccessToken且未过期, 返回")
		c.lock.RUnlock()
		return c.accessToken, nil
	}
	c.lock.RUnlock() // 释放之前的读取锁
	c.lock.Lock()    // 获取写入锁
	defer c.lock.Unlock()
	// log.Print("重新申请AccessToken")
	// 再次检查
	if c.accessToken != "" && time.Now().Before(c.expireAt) {
		return c.accessToken, nil
	}
	// 获取新的Token
	initData := fmt.Appendf(nil, `{"appId":"%s", "clientSecret":"%s"}`, c.AppID, c.AppSecret)
	// log.Printf("请求数据: %v", initData)
	resp, err := http.Post("https://bots.qq.com/app/getAppAccessToken", "application/json", bytes.NewBuffer(initData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// log.Print("请求完毕")
	// 判断状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Request failed with code: %v", resp.StatusCode)
	}
	// 解析数据
	type TokenData struct {
		AccessToken string `json:"access_token"`
		ExpireTime  string `json:"expires_in"`
	}
	var tokenData TokenData
	err = json.NewDecoder(resp.Body).Decode(&tokenData)
	if err != nil {
		return "", fmt.Errorf("Decode json failed: %w", err)
	}
	if tokenData.AccessToken == "" {
		return "", fmt.Errorf("Access token in data is null")
	}
	expriedTime, err := strconv.Atoi(tokenData.ExpireTime)
	if err != nil {
		return "", fmt.Errorf("Incorrect experie_time")
	}
	buffer := 50 // 还有50秒的时候就刷新
	remaining := time.Duration(expriedTime) * time.Second
	c.expireAt = time.Now().Add(remaining - (time.Duration(buffer) * time.Second))
	c.accessToken = tokenData.AccessToken
	// log.Printf("生成AccessToken完毕: %v", c.accessToken)
	return tokenData.AccessToken, nil
}

// 发送群消息
func (c *Client) SendGroupMessage(msg structers.Message, groupId string) error {
	// 从消息生成JSON
	data := msg.GenerateJSON()
	// 构造请求
	req, err := http.NewRequest("POST", fmt.Sprintf("%v/v2/groups/%v/messages", c.ProxyAPI, groupId), bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// 生成Token
	token, err := c.GetAccessToken()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("QQBot %v", token))
	// 发送请求
	resp, err := c.Request.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 读取响应
	if resp.StatusCode == http.StatusOK {
		return nil
	} else {
		return fmt.Errorf("Request failed with code: %v", resp.StatusCode)
	}
}

// 发送私信消息
func (c *Client) SendPrivateMessage(msg structers.Message, userId string) error {
	// 从消息生成JSON
	data := msg.GenerateJSON()
	// 构造请求
	req, err := http.NewRequest("POST", fmt.Sprintf("%v/v2/users/%v/messages", c.ProxyAPI, userId), bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// 生成Token
	token, err := c.GetAccessToken()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("QQBot %v", token))
	// 发送请求
	resp, err := c.Request.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 读取响应
	if resp.StatusCode == http.StatusOK {
		return nil
	} else {
		return fmt.Errorf("Request failed with code: %v", resp.StatusCode)
	}
}
