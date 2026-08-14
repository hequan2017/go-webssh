package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestApplicationHandler(t *testing.T) {
	assets := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>go-webssh</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	cfg := &Config{Host: "127.0.0.1", Port: 22, User: "root", Addr: ":8080", DataDir: t.TempDir(), AdminUser: "admin", AdminPassword: "very-secure-password", SessionTTL: 12 * time.Hour, MaxUploadBytes: 10 << 20}
	app, err := NewApplication(cfg, assets)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	securityResponse, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	securityResponse.Body.Close()
	if securityResponse.Header.Get("Content-Security-Policy") == "" || securityResponse.Header.Get("Permissions-Policy") == "" {
		t.Fatal("安全响应头缺失")
	}
	if securityResponse.Header.Get("Cache-Control") != "" {
		t.Fatal("健康检查不应被静态资源缓存策略影响")
	}

	tests := []struct {
		name        string
		path        string
		status      int
		contentType string
		body        string
	}{
		{name: "首页", path: "/", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: "go-webssh"},
		{name: "健康检查", path: "/healthz", status: http.StatusOK, contentType: "application/json; charset=utf-8", body: `"service":"go-webssh-bastion"`},
		{name: "静态资源", path: "/static/app.css", status: http.StatusOK, contentType: "text/css; charset=utf-8", body: "body{}"},
		{name: "不存在", path: "/missing", status: http.StatusNotFound, contentType: "text/plain; charset=utf-8", body: "404 page not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := http.Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}

			if response.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.status)
			}
			if got := response.Header.Get("Content-Type"); got != tt.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if !strings.Contains(string(body), tt.body) {
				t.Fatalf("body = %q, want to contain %q", body, tt.body)
			}
		})
	}
}
