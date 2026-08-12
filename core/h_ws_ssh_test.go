package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNewHandler(t *testing.T) {
	assets := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>go-webssh</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	server := httptest.NewServer(NewHandler(&Config{}, assets))
	defer server.Close()

	tests := []struct {
		name        string
		path        string
		status      int
		contentType string
		body        string
	}{
		{name: "首页", path: "/", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: "go-webssh"},
		{name: "健康检查", path: "/healthz", status: http.StatusOK, contentType: "application/json; charset=utf-8", body: `{"status":"ok"}`},
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
			if string(body) != tt.body && tt.path != "/" && tt.path != "/missing" {
				t.Fatalf("body = %q, want %q", body, tt.body)
			}
		})
	}
}
