package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

func (a *Application) assetSSHConfig(user User, assetID string) (*Config, Asset, error) {
	asset, err := a.store.Asset(assetID)
	if err != nil {
		return nil, Asset{}, err
	}
	if !asset.Enabled {
		return nil, Asset{}, fmt.Errorf("资产已禁用")
	}
	if !canAccessAsset(user, asset) {
		return nil, Asset{}, fmt.Errorf("无权访问该资产")
	}
	credential, secret, passphrase, err := a.store.CredentialSecret(asset.CredentialID)
	if err != nil {
		return nil, Asset{}, fmt.Errorf("读取资产凭据失败: %w", err)
	}
	cfg := &Config{Host: asset.Host, Port: asset.Port, User: asset.Username}
	switch credential.Type {
	case CredentialPassword:
		cfg.Password = string(secret)
	case CredentialPrivateKey:
		cfg.KeyData = secret
		cfg.KeyPassphrase = passphrase
	default:
		return nil, Asset{}, fmt.Errorf("不支持的凭据类型")
	}
	cfg.HostKeyFingerprint = asset.HostKeyFingerprint
	return cfg, asset, nil
}

func (a *Application) testAsset(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	cfg, asset, err := a.assetSSHConfig(user, r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	client, err := NewSshClient(cfg)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		_ = a.store.AppendAudit(a.auditFor(r, "asset.test", "asset", asset.ID, false, map[string]any{"latency_ms": latency, "error": err.Error()}))
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = client.Close()
	_ = a.store.AppendAudit(a.auditFor(r, "asset.test", "asset", asset.ID, true, map[string]any{"latency_ms": latency}))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "latency_ms": latency, "target": asset.Username + "@" + asset.Host})
}

func (a *Application) wsAsset(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	assetID := r.PathValue("id")
	sshConfig, asset, err := a.assetSSHConfig(user, assetID)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	sessionID, err := newID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	recorder, recordingName, err := newSessionRecorder(a.cfg.RecordingDir(), sessionID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "创建会话记录失败")
		return
	}
	defer recorder.Close()
	record := SessionRecord{
		ID: sessionID, UserID: user.ID, Username: user.Username, AssetID: asset.ID, AssetName: asset.Name,
		ClientIP: clientIP(r), Status: "connecting", RecordingPath: recordingName, StartedAt: time.Now().UTC(),
	}
	if err := a.store.AddSession(record); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存会话失败")
		return
	}
	sessionContext, cancel := context.WithCancel(r.Context())
	r = r.WithContext(sessionContext)
	a.active.Add(sessionID, asset.ID, cancel)
	defer func() {
		cancel()
		a.active.Remove(sessionID)
	}()
	connected := false
	tracker := &commandTracker{onCommand: func(command string) {
		_ = a.store.AppendAudit(a.auditFor(r, "ssh.command", "session", sessionID, true, map[string]any{"command": command, "asset_id": asset.ID}))
	}}
	err = serveSSHWebSocket(w, r, sshConfig, sshHooks{
		OnConnected: func() {
			connected = true
			_ = a.store.UpdateSessionStatus(sessionID, "active")
			_ = a.store.AppendAudit(a.auditFor(r, "ssh.connect", "asset", asset.ID, true, map[string]any{"session_id": sessionID, "asset": asset.Name}))
		},
		OnInput: func(data []byte) {
			recorder.Write("input", data)
			tracker.Write(data)
		},
		OnOutput: func(data []byte) { recorder.Write("output", data) },
	})
	status := "closed"
	if !connected || (err != nil && !errors.Is(err, r.Context().Err()) && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived)) {
		status = "failed"
	}
	if latest, latestErr := a.store.Session(sessionID); latestErr == nil && latest.Status == "terminating" {
		status = "terminated"
	}
	if !connected {
		detail := map[string]any{"session_id": sessionID, "asset": asset.Name}
		if err != nil {
			detail["error"] = err.Error()
		}
		_ = a.store.AppendAudit(a.auditFor(r, "ssh.connect", "asset", asset.ID, false, detail))
	}
	_ = a.store.EndSession(sessionID, status)
	_ = a.store.AppendAudit(a.auditFor(r, "ssh.disconnect", "asset", asset.ID, status == "closed", map[string]any{"session_id": sessionID, "status": status}))
}

func (a *Application) downloadRecording(w http.ResponseWriter, r *http.Request) {
	record, err := a.store.Session(r.PathValue("id"))
	if err != nil || record.RecordingPath == "" || filepath.Base(record.RecordingPath) != record.RecordingPath {
		writeAPIError(w, http.StatusNotFound, "会话录像不存在")
		return
	}
	fullPath := filepath.Join(a.cfg.RecordingDir(), record.RecordingPath)
	if info, statErr := os.Stat(fullPath); statErr != nil || info.IsDir() {
		writeAPIError(w, http.StatusNotFound, "会话录像不存在")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", record.ID+".jsonl"))
	http.ServeFile(w, r, fullPath)
	_ = a.store.AppendAudit(a.auditFor(r, "recording.download", "session", record.ID, true, nil))
}
