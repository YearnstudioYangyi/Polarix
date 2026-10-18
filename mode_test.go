package main

import (
	"Plrx/lib/config"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModeSelection(t *testing.T) {
	client := qqapi.Init("test-app", "test-secret", "https://api.bot.qq.com", requests.Init(1))

	// 1. 测试 Webhook 模式
	t.Run("WebhookMode", func(t *testing.T) {
		cfg := config.AppConfig{
			Mode:      "webhook",
			AppSecret: "test-secret",
		}
		router, wsClient := setupRouter(cfg, &client)
		if wsClient != nil {
			t.Errorf("expected wsClient to be nil in webhook mode")
		}

		req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		// 签名中间件拦截应该返回 401 Unauthorized，表明 /webhook 路由已注册并生效
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected /webhook to return 401 Unauthorized without signature, got %d", rec.Code)
		}
	})

	// 2. 测试 WebSocket 模式
	t.Run("WebSocketMode", func(t *testing.T) {
		cfg := config.AppConfig{
			Mode: "websocket",
			WebSocket: config.WebSocketConfig{
				Intents: 100663296,
				Shard:   []int{0, 1},
			},
		}
		router, wsClient := setupRouter(cfg, &client)
		if wsClient == nil {
			t.Fatalf("expected wsClient to be created in websocket mode")
		}

		// /webhook 在纯 websocket 模式下不应注册
		req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected /webhook to return 404 in websocket mode, got %d", rec.Code)
		}

		// 但是插件临时路由和 push 等接口依然正常可用
		pushReq := httptest.NewRequest(http.MethodPost, "/push/unknown/test", nil)
		pushRec := httptest.NewRecorder()
		router.ServeHTTP(pushRec, pushReq)
		// 不是 404，表明路由注册成功
		if pushRec.Code == http.StatusNotFound {
			t.Errorf("expected /push route to be active in websocket mode")
		}
	})

	// 3. 测试 Both 模式
	t.Run("BothMode", func(t *testing.T) {
		cfg := config.AppConfig{
			Mode:      "both",
			AppSecret: "test-secret",
		}
		router, wsClient := setupRouter(cfg, &client)
		if wsClient == nil {
			t.Errorf("expected wsClient to be created in both mode")
		}
		req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected /webhook to return 401 Unauthorized, got %d", rec.Code)
		}
	})

	// 4. 测试默认空模式回退到 Webhook
	t.Run("DefaultFallbackMode", func(t *testing.T) {
		cfg := config.AppConfig{
			Mode:      "",
			AppSecret: "test-secret",
		}
		router, wsClient := setupRouter(cfg, &client)
		if wsClient != nil {
			t.Errorf("expected wsClient to be nil in default mode")
		}
		req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected /webhook to be registered by default")
		}
	})
}
