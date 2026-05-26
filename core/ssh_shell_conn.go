package core

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"io"
)

// wsBufferWriter 线程安全的 buffer，用于收集 SSH 输出
type wsBufferWriter struct {
	buffer bytes.Buffer
	mu     sync.Mutex
}

func (w *wsBufferWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

const (
	wsMsgCmd    = "cmd"
	wsMsgResize = "resize"
)

// wsMsg WebSocket 消息格式
type wsMsg struct {
	Type string `json:"type"`
	Cmd  string `json:"cmd"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// SshConn 管理 SSH 会话的输入输出
type SshConn struct {
	StdinPipe   io.WriteCloser
	ComboOutput *wsBufferWriter
	Session     *ssh.Session
}

// flushComboOutput 将 SSH 输出发送到 WebSocket
func flushComboOutput(w *wsBufferWriter, wsConn *websocket.Conn) error {
	if w.buffer.Len() != 0 {
		err := wsConn.WriteMessage(websocket.TextMessage, w.buffer.Bytes())
		if err != nil {
			return err
		}
		w.buffer.Reset()
	}
	return nil
}

// NewSshConn 创建 SSH shell 会话
func NewSshConn(cols, rows int, sshClient *ssh.Client) (*SshConn, error) {
	sshSession, err := sshClient.NewSession()
	if err != nil {
		return nil, err
	}

	stdinP, err := sshSession.StdinPipe()
	if err != nil {
		return nil, err
	}

	comboWriter := new(wsBufferWriter)
	sshSession.Stdout = comboWriter
	sshSession.Stderr = comboWriter

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSession.RequestPty("xterm", rows, cols, modes); err != nil {
		return nil, err
	}
	if err := sshSession.Shell(); err != nil {
		return nil, err
	}
	return &SshConn{StdinPipe: stdinP, ComboOutput: comboWriter, Session: sshSession}, nil
}

func (s *SshConn) Close() {
	if s.Session != nil {
		s.Session.Close()
	}
}

// ReceiveWsMsg 从 WebSocket 读取消息，区分命令和 resize
func (ssConn *SshConn) ReceiveWsMsg(wsConn *websocket.Conn, exitCh chan bool) {
	defer setQuit(exitCh)
	for {
		select {
		case <-exitCh:
			return
		default:
			_, wsData, err := wsConn.ReadMessage()
			if err != nil {
				logrus.WithError(err).Error("reading webSocket message failed")
				return
			}

			// 尝试解析为 JSON（resize 消息），否则作为原始命令处理
			var msg wsMsg
			if json.Unmarshal(wsData, &msg) == nil && (msg.Type == wsMsgResize || msg.Type == wsMsgCmd) {
				switch msg.Type {
				case wsMsgResize:
					if msg.Cols > 0 && msg.Rows > 0 {
						if err := ssConn.Session.WindowChange(msg.Rows, msg.Cols); err != nil {
							logrus.WithError(err).Error("ssh pty resize failed")
						}
					}
				case wsMsgCmd:
					if _, err := ssConn.StdinPipe.Write([]byte(msg.Cmd)); err != nil {
						logrus.WithError(err).Error("ws cmd write to ssh.stdin failed")
					}
				}
			} else {
				// 原始终端输入，直接写入 SSH stdin
				if _, err := ssConn.StdinPipe.Write(wsData); err != nil {
					logrus.WithError(err).Error("ws data write to ssh.stdin failed")
				}
			}
		}
	}
}

// SendComboOutput 每 120ms 将 SSH 输出发送到 WebSocket
func (ssConn *SshConn) SendComboOutput(wsConn *websocket.Conn, exitCh chan bool) {
	defer setQuit(exitCh)
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := flushComboOutput(ssConn.ComboOutput, wsConn); err != nil {
				logrus.WithError(err).Error("ssh output send to websocket failed")
				return
			}
		case <-exitCh:
			return
		}
	}
}

// SessionWait 等待 SSH 会话结束
func (ssConn *SshConn) SessionWait(quitChan chan bool) {
	if err := ssConn.Session.Wait(); err != nil {
		logrus.WithError(err).Error("ssh session wait failed")
	}
	setQuit(quitChan)
}

func setQuit(ch chan bool) {
	select {
	case ch <- true:
	default:
	}
}
