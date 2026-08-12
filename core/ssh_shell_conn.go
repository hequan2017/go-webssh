package core

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

const (
	wsMsgCmd    = "cmd"
	wsMsgResize = "resize"
)

type wsMsg struct {
	Type string `json:"type"`
	Cmd  string `json:"cmd"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type outputWriter struct {
	ch   chan<- []byte
	done <-chan struct{}
}

func (w outputWriter) Write(data []byte) (int, error) {
	copyOfData := append([]byte(nil), data...)
	select {
	case w.ch <- copyOfData:
		return len(data), nil
	case <-w.done:
		return 0, io.ErrClosedPipe
	}
}

type SshConn struct {
	StdinPipe io.WriteCloser
	Session   *ssh.Session
	Output    <-chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func NewSshConn(cols, rows int, client *ssh.Client) (*SshConn, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("创建 SSH 会话失败: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("创建 SSH 输入流失败: %w", err)
	}

	output := make(chan []byte, 32)
	done := make(chan struct{})
	writer := outputWriter{ch: output, done: done}
	session.Stdout = writer
	session.Stderr = writer

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("申请终端失败: %w", err)
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("启动 shell 失败: %w", err)
	}

	return &SshConn{StdinPipe: stdin, Session: session, Output: output, done: done}, nil
}

func (c *SshConn) ReadWebSocket(ws *websocket.Conn) error {
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}

		var message wsMsg
		if json.Unmarshal(data, &message) == nil {
			switch message.Type {
			case wsMsgResize:
				if message.Cols > 0 && message.Rows > 0 {
					if err := c.Session.WindowChange(message.Rows, message.Cols); err != nil {
						return fmt.Errorf("调整终端尺寸失败: %w", err)
					}
				}
				continue
			case wsMsgCmd:
				data = []byte(message.Cmd)
			}
		}

		if _, err := c.StdinPipe.Write(data); err != nil {
			return fmt.Errorf("写入 SSH 终端失败: %w", err)
		}
	}
}

func (c *SshConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)
		err = c.Session.Close()
	})
	return err
}
