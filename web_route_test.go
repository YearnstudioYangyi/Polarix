package main

import (
	appcontext "Plrx/lib/context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTemporaryRouteReturnsContentAndConsumesOneTimeLink(t *testing.T) {
	ctx := &appcontext.Context{}
	ctx.BindStorage("web-test", "test")
	path, _, err := ctx.RegisterTemporaryRoute(appcontext.TemporaryRouteOptions{TTL: time.Minute, OneTime: true, ContentType: "text/html; charset=utf-8"}, func(*http.Request) (any, error) { return "<h1>verified</h1>", nil })
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(path, "/")
	first := httptest.NewRecorder()
	appcontext.ServeTemporaryRoute(first, httptest.NewRequest(http.MethodGet, path, nil), parts[2], parts[3])
	if first.Code != http.StatusOK || first.Body.String() != "<h1>verified</h1>" {
		t.Fatalf("unexpected first response: %d %q", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	second := httptest.NewRecorder()
	appcontext.ServeTemporaryRoute(second, httptest.NewRequest(http.MethodGet, path, nil), parts[2], parts[3])
	if second.Code != http.StatusNotFound {
		t.Fatalf("second response = %d, want 404", second.Code)
	}
}

func TestTemporaryRouteRejectsWrongMethodAndExpiredRoute(t *testing.T) {
	ctx := &appcontext.Context{}
	ctx.BindStorage("web-method", "test")
	path, remove, err := ctx.RegisterTemporaryRoute(appcontext.TemporaryRouteOptions{TTL: time.Minute, Method: http.MethodPost}, func(*http.Request) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	parts := strings.Split(path, "/")
	recorder := httptest.NewRecorder()
	appcontext.ServeTemporaryRoute(recorder, httptest.NewRequest(http.MethodGet, path, nil), parts[2], parts[3])
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("wrong-method response = %d, allow %q", recorder.Code, recorder.Header().Get("Allow"))
	}
	expiredCtx := &appcontext.Context{}
	expiredCtx.BindStorage("web-expired", "test")
	expiredPath, _, err := expiredCtx.RegisterTemporaryRoute(appcontext.TemporaryRouteOptions{TTL: time.Nanosecond}, func(*http.Request) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	expiredParts := strings.Split(expiredPath, "/")
	expired := httptest.NewRecorder()
	appcontext.ServeTemporaryRoute(expired, httptest.NewRequest(http.MethodGet, expiredPath, nil), expiredParts[2], expiredParts[3])
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired response = %d, want 404", expired.Code)
	}
}
