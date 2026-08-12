package core

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

func NewHandler(cfg *Config, assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	staticFiles, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /", serveIndex(assets))
	mux.HandleFunc("GET /ws/{id}", wsSSH(cfg))
	return securityHeaders(mux)
}

func serveIndex(assets fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, err := fs.ReadFile(assets, "web/html/index.html")
		if err != nil {
			http.Error(w, "页面资源不可用", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}
}

func wsSSH(cfg *Config) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     sameOrigin,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		cols, err := positiveInt(r.URL.Query().Get("cols"), 120)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rows, err := positiveInt(r.URL.Query().Get("rows"), 32)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		client, err := NewSshClient(cfg)
		if err != nil {
			closeWebSocket(ws, err)
			return
		}
		defer client.Close()

		terminal, err := NewSshConn(cols, rows, client)
		if err != nil {
			closeWebSocket(ws, err)
			return
		}
		defer terminal.Close()

		readDone := make(chan error, 1)
		waitDone := make(chan error, 1)
		go func() { readDone <- terminal.ReadWebSocket(ws) }()
		go func() { waitDone <- terminal.Session.Wait() }()

		for {
			select {
			case data := <-terminal.Output:
				if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
					return
				}
			case err := <-readDone:
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					slog.Debug("WebSocket 输入结束", "error", err)
				}
				return
			case err := <-waitDone:
				if err != nil {
					slog.Debug("SSH 会话结束", "error", err)
				}
				return
			case <-r.Context().Done():
				return
			}
		}
	}
}

func positiveInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > 1000 {
		return 0, fmt.Errorf("终端尺寸必须是 1 到 1000 的整数")
	}
	return parsed, nil
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host
}

func closeWebSocket(ws *websocket.Conn, err error) {
	message := err.Error()
	if len(message) > 120 {
		message = message[:120]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	_ = ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseInternalServerErr, message),
		writeDeadline(),
	)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
