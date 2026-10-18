package config

import (
	"encoding/json"
	"testing"
)

func TestConfigModeParsing(t *testing.T) {
	raw := []byte(`{
		"mode": "websocket",
		"websocket": {
			"intents": 100663296,
			"shard": [0, 2],
			"gateway": "wss://test.gateway"
		}
	}`)

	var cfg AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if cfg.Mode != "websocket" {
		t.Errorf("mode = %q, want 'websocket'", cfg.Mode)
	}
	if cfg.WebSocket.Intents != 100663296 {
		t.Errorf("intents = %d, want 100663296", cfg.WebSocket.Intents)
	}
	if len(cfg.WebSocket.Shard) != 2 || cfg.WebSocket.Shard[0] != 0 || cfg.WebSocket.Shard[1] != 2 {
		t.Errorf("shard = %v, want [0, 2]", cfg.WebSocket.Shard)
	}
	if cfg.WebSocket.Gateway != "wss://test.gateway" {
		t.Errorf("gateway = %q, want 'wss://test.gateway'", cfg.WebSocket.Gateway)
	}
}

func TestConfigDefaultMode(t *testing.T) {
	raw := []byte(`{
		"appid": "123"
	}`)

	var cfg AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if cfg.Mode == "" {
		cfg.Mode = "webhook"
	}
	if cfg.Mode != "webhook" {
		t.Errorf("expected default mode 'webhook', got %q", cfg.Mode)
	}
}
