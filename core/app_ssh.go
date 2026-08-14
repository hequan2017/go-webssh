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

func (a *Application) sshConfigForAsset(asset Asset) (*Config, error) {
	credential, secret, passphrase, err := a.store.CredentialSecret(asset.CredentialID)
	if err != nil {
		return nil, fmt.Errorf("读取资产凭据失败: %w", err)
	}
	cfg := &Config{Host: asset.Host, Port: asset.Port, User: asset.Username}
	switch credential.Type {
	case CredentialPassword:
		cfg.Password = string(secret)
	case CredentialPrivateKey:
		cfg.KeyData = secret
		cfg.KeyPassphrase = passphrase
	default:
		return nil, fmt.Errorf("不支持的凭据类型")
	}
	cfg.HostKeyFingerprint = asset.HostKeyFingerprint
	return cfg, nil
}

// assetSSHChain 解析资产及其跳板机链路。返回值按连接顺序排列（跳板机在前、目标资产在最后），
// jumps 仅包含跳板机资产；同时校验每一跳的启用状态和当前用户对其资产组的访问授权。
func (a *Application) assetSSHChain(user User, assetID string) ([]*Config, Asset, []Asset, error) {
	asset, err := a.store.Asset(assetID)
	if err != nil {
		return nil, Asset{}, nil, err
	}
	if !asset.Enabled {
		return nil, Asset{}, nil, fmt.Errorf("资产已禁用")
	}
	if !canAccessAsset(user, asset) {
		return nil, Asset{}, nil, fmt.Errorf("无权访问该资产")
	}
	hops := []Asset{asset}
	visited := map[string]bool{asset.ID: true}
	for current := asset; current.JumpAssetID != ""; {
		if len(hops) > MaxJumpHops {
			return nil, Asset{}, nil, fmt.Errorf("跳板链路不能超过 %d 层", MaxJumpHops)
		}
		jump, err := a.store.Asset(current.JumpAssetID)
		if err != nil {
			return nil, Asset{}, nil, fmt.Errorf("跳板机资产不存在")
		}
		if !jump.Enabled {
			return nil, Asset{}, nil, fmt.Errorf("跳板机 %s 已禁用", jump.Name)
		}
		if !canAccessAsset(user, jump) {
			return nil, Asset{}, nil, fmt.Errorf("无权访问跳板机 %s", jump.Name)
		}
		if visited[jump.ID] {
			return nil, Asset{}, nil, fmt.Errorf("跳板链路形成循环")
		}
		visited[jump.ID] = true
		hops = append([]Asset{jump}, hops...)
		current = jump
	}
	chain := make([]*Config, 0, len(hops))
	for _, hop := range hops {
		cfg, err := a.sshConfigForAsset(hop)
		if err != nil {
			return nil, Asset{}, nil, err
		}
		chain = append(chain, cfg)
	}
	return chain, hops[len(hops)-1], hops[:len(hops)-1], nil
}

func jumpNames(jumps []Asset) []string {
	if len(jumps) == 0 {
		return nil
	}
	names := make([]string, 0, len(jumps))
	for _, jump := range jumps {
		names = append(names, jump.Name)
	}
	return names
}

func (a *Application) testAsset(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	chain, asset, jumps, err := a.assetSSHChain(user, r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	client, err := NewSshClientChain(chain)
	latency := time.Since(started).Milliseconds()
	detail := map[string]any{"latency_ms": latency, "jump_path": jumpNames(jumps)}
	if err != nil {
		detail["error"] = err.Error()
		_ = a.store.AppendAudit(a.auditFor(r, "asset.test", "asset", asset.ID, false, detail))
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = client.Close()
	_ = a.store.AppendAudit(a.auditFor(r, "asset.test", "asset", asset.ID, true, detail))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "latency_ms": latency, "target": asset.Username + "@" + asset.Host, "jump_path": jumpNames(jumps)})
}

func (a *Application) wsAsset(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	assetID := r.PathValue("id")
	chain, asset, jumps, err := a.assetSSHChain(user, assetID)
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
		JumpNames: jumpNames(jumps), ClientIP: clientIP(r), Status: "connecting", RecordingPath: recordingName, StartedAt: time.Now().UTC(),
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
	connectDetail := map[string]any{"session_id": sessionID, "asset": asset.Name, "jump_path": jumpNames(jumps)}
	err = serveSSHWebSocket(w, r, chain, sshHooks{
		OnConnected: func() {
			connected = true
			_ = a.store.UpdateSessionStatus(sessionID, "active")
			_ = a.store.AppendAudit(a.auditFor(r, "ssh.connect", "asset", asset.ID, true, connectDetail))
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
		detail := map[string]any{"session_id": sessionID, "asset": asset.Name, "jump_path": jumpNames(jumps)}
		if err != nil {
			detail["error"] = err.Error()
		}
		_ = a.store.AppendAudit(a.auditFor(r, "ssh.connect", "asset", asset.ID, false, detail))
	}
	_ = a.store.EndSession(sessionID, status)
	_ = a.store.AppendAudit(a.auditFor(r, "ssh.disconnect", "asset", asset.ID, status == "closed", map[string]any{"session_id": sessionID, "status": status, "jump_path": jumpNames(jumps)}))
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
