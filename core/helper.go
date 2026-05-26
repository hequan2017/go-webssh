package core

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

func jsonError(c *gin.Context, msg interface{}) {
	c.AbortWithStatusJSON(200, gin.H{"ok": false, "msg": msg})
}

func handleError(c *gin.Context, err error) bool {
	if err != nil {
		jsonError(c, err.Error())
		return true
	}
	return false
}

func wshandleError(ws *websocket.Conn, err error) bool {
	if err != nil {
		logrus.WithError(err).Error("websocket error")
		dt := time.Now().Add(time.Second)
		if err := ws.WriteControl(websocket.CloseMessage, []byte(err.Error()), dt); err != nil {
			logrus.WithError(err).Error("websocket write control message failed")
		}
		return true
	}
	return false
}
