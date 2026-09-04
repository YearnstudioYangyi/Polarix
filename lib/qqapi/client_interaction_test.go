package qqapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Plrx/lib/requests"
)

func TestInteracteCallbackSendsSuccessCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/interactions/interaction-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "QQBot test-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}

		var payload struct {
			Code int `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Code != 0 {
			t.Errorf("code = %d, want 0", payload.Code)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &Client{
		ProxyAPI:    server.URL,
		Request:     requests.Init(1),
		accessToken: "test-token",
		expireAt:    time.Now().Add(time.Hour),
	}
	if err := client.InteracteCallback("interaction-1"); err != nil {
		t.Fatalf("InteracteCallback returned error: %v", err)
	}
}
