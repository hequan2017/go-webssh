package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHandleError_Nil(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if handleError(c, nil) != false {
		t.Error("handleError(nil) should return false")
	}
}

func TestHandleError_WithErr(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	result := handleError(c, errors.New("test error"))
	if result != true {
		t.Error("handleError(err) should return true")
	}
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test error") {
		t.Errorf("body = %q, should contain 'test error'", body)
	}
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("body = %q, should contain ok:false", body)
	}
}

func TestWshandleError_Nil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		ws, _ := upgrader.Upgrade(w, r, nil)
		defer ws.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if wshandleError(ws, nil) != false {
		t.Error("wshandleError(nil) should return false")
	}
}

func TestWshandleError_WithErr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		ws, _ := upgrader.Upgrade(w, r, nil)
		defer ws.Close()
		// 读取客户端发来的 close 消息
		ws.ReadMessage()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	result := wshandleError(ws, errors.New("ws test error"))
	if result != true {
		t.Error("wshandleError(err) should return true")
	}
}
