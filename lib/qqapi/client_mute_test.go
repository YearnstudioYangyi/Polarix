package qqapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Plrx/lib/requests"
)

func TestSetGroupMemberMute(t *testing.T) {
	var payload struct {
		Members []memberMuteState `json:"members"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v2/groups/group-1/restrict_chat_setting" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "QQBot test-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
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
	if err := client.BanGroupMember("group-1", "member-1", "2026-08-31T12:00:00+08:00"); err != nil {
		t.Fatalf("BanGroupMember returned error: %v", err)
	}

	if len(payload.Members) != 1 {
		t.Fatalf("members length = %d, want 1", len(payload.Members))
	}
	member := payload.Members[0]
	if member.Operation != MemberMuteAdd || member.MemberOpenID != "member-1" || member.MuteExpireAt != "2026-08-31T12:00:00+08:00" {
		t.Errorf("member = %+v", member)
	}
}
