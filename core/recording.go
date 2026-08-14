package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type recordingFrame struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Data      string    `json:"data"`
}

type sessionRecorder struct {
	mu   sync.Mutex
	file *os.File
}

func newSessionRecorder(directory, sessionID string) (*sessionRecorder, string, error) {
	name := sessionID + ".jsonl"
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, "", err
	}
	return &sessionRecorder{file: file}, name, nil
}

func (r *sessionRecorder) Write(direction string, data []byte) {
	frame := recordingFrame{Time: time.Now().UTC(), Direction: direction, Data: base64.StdEncoding.EncodeToString(data)}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.file.Write(append(encoded, '\n'))
}

func (r *sessionRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

type commandTracker struct {
	mu        sync.Mutex
	buffer    []byte
	onCommand func(string)
}

func (t *commandTracker) Write(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, value := range data {
		switch value {
		case '\r', '\n':
			if len(t.buffer) > 0 {
				command := string(t.buffer)
				if len(command) > 512 {
					command = command[:512]
				}
				t.onCommand(command)
				t.buffer = t.buffer[:0]
			}
		case 0x7f, 0x08:
			if len(t.buffer) > 0 {
				t.buffer = t.buffer[:len(t.buffer)-1]
			}
		default:
			if value >= 0x20 && value != 0x1b && len(t.buffer) < 4096 {
				t.buffer = append(t.buffer, value)
			}
		}
	}
}
