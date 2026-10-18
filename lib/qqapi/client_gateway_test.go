package qqapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Plrx/lib/requests"
)

func TestGetGatewaySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "QQBot test-token" {
			t.Errorf("expected authorization 'QQBot test-token', got %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/gateway/bot" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":    "wss://api.bot.qq.com/websocket/",
				"shards": 2,
				"session_start_limit": map[string]any{
					"total":           1000,
					"remaining":       999,
					"reset_after":     86400000,
					"max_concurrency": 1,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &Client{
		ProxyAPI:    server.URL,
		Request:     requests.Init(1),
		accessToken: "test-token",
		expireAt:    time.Now().Add(time.Hour),
	}

	info, err := client.GetGateway()
	if err != nil {
		t.Fatalf("GetGateway failed: %v", err)
	}
	if info.URL != "wss://api.bot.qq.com/websocket/" {
		t.Errorf("url = %q, want %q", info.URL, "wss://api.bot.qq.com/websocket/")
	}
	if info.Shards != 2 {
		t.Errorf("shards = %d, want 2", info.Shards)
	}
}

func TestGetGatewayFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gateway/bot" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/gateway" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "wss://api.bot.qq.com/websocket/",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &Client{
		ProxyAPI:    server.URL,
		Request:     requests.Init(1),
		accessToken: "test-token",
		expireAt:    time.Now().Add(time.Hour),
	}

	info, err := client.GetGateway()
	if err != nil {
		t.Fatalf("GetGateway fallback failed: %v", err)
	}
	if info.URL != "wss://api.bot.qq.com/websocket/" {
		t.Errorf("url = %q, want %q", info.URL, "wss://api.bot.qq.com/websocket/")
	}
}
