package core

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	webSocketReadLimit    = 64 << 10
	webSocketPongWait     = 60 * time.Second
	webSocketPingInterval = 25 * time.Second
)

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
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	}
}

type sshHooks struct {
	OnConnected func()
	OnInput     func([]byte)
	OnOutput    func([]byte)
}

func serveSSHWebSocket(w http.ResponseWriter, r *http.Request, cfg *Config, hooks sshHooks) error {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     sameOrigin,
	}
	cols, err := positiveInt(r.URL.Query().Get("cols"), 120)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}
	rows, err := positiveInt(r.URL.Query().Get("rows"), 32)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(webSocketReadLimit)
	_ = ws.SetReadDeadline(time.Now().Add(webSocketPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(webSocketPongWait))
	})

	client, err := NewSshClient(cfg)
	if err != nil {
		closeWebSocket(ws, err)
		return err
	}
	defer client.Close()

	terminal, err := NewSshConn(cols, rows, client)
	if err != nil {
		closeWebSocket(ws, err)
		return err
	}
	defer terminal.Close()
	terminal.OnInput = hooks.OnInput
	if hooks.OnConnected != nil {
		hooks.OnConnected()
	}

	readDone := make(chan error, 1)
	waitDone := make(chan error, 1)
	go func() { readDone <- terminal.ReadWebSocket(ws) }()
	go func() { waitDone <- terminal.Session.Wait() }()
	pingTicker := time.NewTicker(webSocketPingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case data := <-terminal.Output:
			if hooks.OnOutput != nil {
				hooks.OnOutput(data)
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return err
			}
		case err := <-readDone:
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("WebSocket 输入结束", "error", err)
			}
			return err
		case err := <-waitDone:
			if err != nil {
				slog.Debug("SSH 会话结束", "error", err)
			}
			return err
		case <-pingTicker.C:
			if err := ws.WriteControl(websocket.PingMessage, nil, writeDeadline()); err != nil {
				return err
			}
		case <-r.Context().Done():
			return r.Context().Err()
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
