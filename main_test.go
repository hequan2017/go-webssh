package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedFrontendAssets(t *testing.T) {
	for _, path := range []string{
		"web/html/index.html",
		"static/css/app.css",
		"static/css/xterm.min.css",
		"static/js/app.js",
		"static/js/xterm.min.js",
	} {
		if _, err := fs.ReadFile(assets, path); err != nil {
			t.Fatalf("读取嵌入资源 %q 失败: %v", path, err)
		}
	}

	page, err := fs.ReadFile(assets, "web/html/index.html")
	if err != nil {
		t.Fatal(err)
	}
	content := string(page)
	for _, expected := range []string{
		`name="go-webssh-mode" content="server"`,
		`href="./static/css/app.css"`,
		`src="./static/js/app.js"`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("首页缺少 %q", expected)
		}
	}
}
