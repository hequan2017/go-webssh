package core

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

var upGrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024 * 1024 * 10,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WsSsh 处理 WebSocket 连接，建立 SSH 桥接
func WsSsh(c *gin.Context) {
	wsConn, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if handleError(c, err) {
		return
	}
	defer wsConn.Close()

	cols, err := strconv.Atoi(c.DefaultQuery("cols", "120"))
	if wshandleError(wsConn, err) {
		return
	}
	rows, err := strconv.Atoi(c.DefaultQuery("rows", "32"))
	if wshandleError(wsConn, err) {
		return
	}

	client, err := NewSshClient(AppConfig)
	if wshandleError(wsConn, err) {
		return
	}
	defer client.Close()

	ssConn, err := NewSshConn(cols, rows, client)
	if wshandleError(wsConn, err) {
		return
	}
	defer ssConn.Close()

	quitChan := make(chan bool, 3)

	go ssConn.ReceiveWsMsg(wsConn, quitChan)
	go ssConn.SendComboOutput(wsConn, quitChan)
	go ssConn.SessionWait(quitChan)

	<-quitChan
	logrus.Info("websocket finished")
}
