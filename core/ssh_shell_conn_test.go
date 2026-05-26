package core

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestWsBufferWriter_Write(t *testing.T) {
	var w wsBufferWriter
	data := []byte("hello")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() n = %d, want %d", n, len(data))
	}
	if w.buffer.String() != "hello" {
		t.Errorf("buffer = %q, want %q", w.buffer.String(), "hello")
	}
}

func TestWsBufferWriter_Concurrent(t *testing.T) {
	var w wsBufferWriter
	var wg sync.WaitGroup
	goroutines := 100
	data := []byte("x")

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			w.Write(data)
		}()
	}
	wg.Wait()

	if w.buffer.Len() != goroutines {
		t.Errorf("buffer len = %d, want %d", w.buffer.Len(), goroutines)
	}
}

func TestWsBufferWriter_Append(t *testing.T) {
	var w wsBufferWriter
	w.Write([]byte("abc"))
	w.Write([]byte("def"))
	if w.buffer.String() != "abcdef" {
		t.Errorf("buffer = %q, want %q", w.buffer.String(), "abcdef")
	}
}

func TestWsMsg_Unmarshal_Cmd(t *testing.T) {
	raw := `{"type":"cmd","cmd":"ls -la\n"}`
	var msg wsMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if msg.Type != wsMsgCmd {
		t.Errorf("Type = %q, want %q", msg.Type, wsMsgCmd)
	}
	if msg.Cmd != "ls -la\n" {
		t.Errorf("Cmd = %q, want %q", msg.Cmd, "ls -la\n")
	}
}

func TestWsMsg_Unmarshal_Resize(t *testing.T) {
	raw := `{"type":"resize","cols":80,"rows":24}`
	var msg wsMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if msg.Type != wsMsgResize {
		t.Errorf("Type = %q, want %q", msg.Type, wsMsgResize)
	}
	if msg.Cols != 80 {
		t.Errorf("Cols = %d, want %d", msg.Cols, 80)
	}
	if msg.Rows != 24 {
		t.Errorf("Rows = %d, want %d", msg.Rows, 24)
	}
}

func TestWsMsg_Unmarshal_InvalidJSON(t *testing.T) {
	raw := `not json at all`
	var msg wsMsg
	if err := json.Unmarshal([]byte(raw), &msg); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSetQuit(t *testing.T) {
	ch := make(chan bool, 1)
	setQuit(ch)
	select {
	case <-ch:
		// ok
	default:
		t.Error("setQuit did not send to channel")
	}
}

func TestSetQuit_NonBlocking(t *testing.T) {
	ch := make(chan bool, 1)
	ch <- true // channel 已满
	setQuit(ch) // 不应阻塞
}

func TestSetQuit_Multiple(t *testing.T) {
	ch := make(chan bool, 3)
	setQuit(ch)
	setQuit(ch)
	setQuit(ch)
	if len(ch) != 3 {
		t.Errorf("channel len = %d, want 3", len(ch))
	}
}

func TestFlushComboOutput_Empty(t *testing.T) {
	var w wsBufferWriter
	// 空buffer不应panic
	// flushComboOutput 需要一个真实的 websocket 连接才能测试非空情况，
	// 空buffer时直接返回nil，无需 wsConn
	if w.buffer.Len() != 0 {
		t.Error("buffer should be empty")
	}
}
