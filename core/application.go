package core

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

type Application struct {
	cfg      *Config
	store    *Store
	sessions *sessionManager
	limiter  *loginLimiter
	active   *activeSessionRegistry
	assets   fs.FS
}

func NewApplication(cfg *Config, assets fs.FS) (*Application, error) {
	store, err := NewStore(cfg)
	if err != nil {
		return nil, err
	}
	return &Application{cfg: cfg, store: store, sessions: newSessionManager(cfg.SessionTTL), limiter: newLoginLimiter(), active: newActiveSessionRegistry(), assets: assets}, nil
}

func (a *Application) Handler() http.Handler {
	mux := http.NewServeMux()
	staticFiles, err := fs.Sub(a.assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", noCache(http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles)))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "go-webssh-bastion"})
	})
	mux.HandleFunc("GET /", serveIndex(a.assets))
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.requireAuth(a.logout))
	mux.HandleFunc("GET /api/me", a.requireAuth(a.me))
	mux.HandleFunc("POST /api/me/password", a.requireAuth(a.changePassword))
	mux.HandleFunc("GET /api/assets", a.requireAuth(a.listAssets))
	mux.HandleFunc("POST /api/assets", a.requireRoles(a.saveAsset, RoleAdmin))
	mux.HandleFunc("PUT /api/assets/{id}", a.requireRoles(a.saveAsset, RoleAdmin))
	mux.HandleFunc("DELETE /api/assets/{id}", a.requireRoles(a.deleteAsset, RoleAdmin))
	mux.HandleFunc("POST /api/assets/{id}/test", a.requireRoles(a.testAsset, RoleAdmin))
	mux.HandleFunc("POST /api/discovery", a.requireRoles(a.discoverHosts, RoleAdmin))
	mux.HandleFunc("GET /api/credentials", a.requireRoles(a.listCredentials, RoleAdmin))
	mux.HandleFunc("POST /api/credentials", a.requireRoles(a.saveCredential, RoleAdmin))
	mux.HandleFunc("PUT /api/credentials/{id}", a.requireRoles(a.saveCredential, RoleAdmin))
	mux.HandleFunc("DELETE /api/credentials/{id}", a.requireRoles(a.deleteCredential, RoleAdmin))
	mux.HandleFunc("GET /api/users", a.requireRoles(a.listUsers, RoleAdmin))
	mux.HandleFunc("POST /api/users", a.requireRoles(a.createUser, RoleAdmin))
	mux.HandleFunc("PUT /api/users/{id}", a.requireRoles(a.updateUser, RoleAdmin))
	mux.HandleFunc("DELETE /api/users/{id}", a.requireRoles(a.deleteUser, RoleAdmin))
	mux.HandleFunc("GET /api/audits", a.requireRoles(a.listAudits, RoleAdmin, RoleAuditor))
	mux.HandleFunc("GET /api/sessions", a.requireRoles(a.listSessions, RoleAdmin, RoleAuditor))
	mux.HandleFunc("DELETE /api/sessions/{id}", a.requireRoles(a.terminateSession, RoleAdmin))
	mux.HandleFunc("GET /api/sessions/{id}/recording", a.requireRoles(a.downloadRecording, RoleAdmin, RoleAuditor))
	mux.HandleFunc("GET /ws/{id}", a.requireAuth(a.wsAsset))
	mux.HandleFunc("GET /api/assets/{id}/files", a.requireAuth(a.listFiles))
	mux.HandleFunc("POST /api/assets/{id}/files", a.requireAuth(a.uploadFile))
	mux.HandleFunc("PATCH /api/assets/{id}/files", a.requireAuth(a.renameFile))
	mux.HandleFunc("DELETE /api/assets/{id}/files", a.requireAuth(a.deleteFile))
	mux.HandleFunc("POST /api/assets/{id}/directories", a.requireAuth(a.createDirectory))
	mux.HandleFunc("GET /api/assets/{id}/download", a.requireAuth(a.downloadFile))
	return securityHeaders(a.csrfProtection(mux))
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (a *Application) csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/auth/login" && !sameOrigin(r) {
			writeAPIError(w, http.StatusForbidden, "请求来源无效")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Application) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		userID, ok := a.sessions.Resolve(cookie.Value)
		if !ok {
			clearSessionCookie(w, r)
			writeAPIError(w, http.StatusUnauthorized, "会话已过期")
			return
		}
		user, err := a.store.User(userID)
		if err != nil || !user.Enabled {
			a.sessions.Delete(cookie.Value)
			clearSessionCookie(w, r)
			writeAPIError(w, http.StatusUnauthorized, "用户不可用")
			return
		}
		next(w, withUser(r, user))
	}
}

func (a *Application) requireRoles(next http.HandlerFunc, roles ...Role) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := requestUser(r)
		if !hasRole(user, roles...) {
			writeAPIError(w, http.StatusForbidden, "权限不足")
			return
		}
		next(w, r)
	})
}

func (a *Application) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.limiter.Allow(ip) {
		writeAPIError(w, http.StatusTooManyRequests, "登录失败次数过多，请稍后再试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "登录参数无效")
		return
	}
	user, err := a.store.Authenticate(input.Username, input.Password)
	if err != nil {
		a.limiter.Failure(ip)
		_ = a.store.AppendAudit(AuditEvent{Username: strings.TrimSpace(input.Username), Action: "auth.login", ResourceType: "user", ClientIP: clientIP(r), Success: false})
		writeAPIError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	a.limiter.Success(ip)
	a.store.TouchLogin(user.ID)
	token, expires, err := a.sessions.Create(user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	setSessionCookie(w, r, token, expires)
	_ = a.store.AppendAudit(AuditEvent{UserID: user.ID, Username: user.Username, Action: "auth.login", ResourceType: "user", ResourceID: user.ID, ClientIP: ip, Success: true})
	writeJSON(w, http.StatusOK, user.Public())
}

func (a *Application) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		a.sessions.Delete(cookie.Value)
	}
	user, _ := requestUser(r)
	clearSessionCookie(w, r)
	_ = a.store.AppendAudit(a.auditFor(r, "auth.logout", "user", user.ID, true, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) me(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	writeJSON(w, http.StatusOK, user.Public())
}

func (a *Application) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "密码参数无效")
		return
	}
	user, _ := requestUser(r)
	if _, err := a.store.Authenticate(user.Username, input.CurrentPassword); err != nil {
		_ = a.store.AppendAudit(a.auditFor(r, "user.password_change", "user", user.ID, false, nil))
		writeAPIError(w, http.StatusBadRequest, "当前密码错误")
		return
	}
	if _, err := a.store.UpdateUser(user.ID, user.Role, user.Enabled, input.NewPassword, user.AssetGroups); err != nil {
		writeStoreError(w, err)
		return
	}
	a.sessions.DeleteUser(user.ID)
	clearSessionCookie(w, r)
	_ = a.store.AppendAudit(a.auditFor(r, "user.password_change", "user", user.ID, true, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) auditFor(r *http.Request, action, resourceType, resourceID string, success bool, detail map[string]any) AuditEvent {
	user, _ := requestUser(r)
	return AuditEvent{UserID: user.ID, Username: user.Username, Action: action, ResourceType: resourceType, ResourceID: resourceID, ClientIP: clientIP(r), Success: success, Detail: detail}
}

func (a *Application) listAssets(w http.ResponseWriter, r *http.Request) {
	user, _ := requestUser(r)
	assets := a.store.Assets()
	filtered := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		if user.Role == RoleAdmin || user.Role == RoleAuditor || canAccessAsset(user, asset) {
			filtered = append(filtered, asset)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (a *Application) saveAsset(w http.ResponseWriter, r *http.Request) {
	var asset Asset
	if err := decodeJSON(w, r, &asset); err != nil {
		writeAPIError(w, http.StatusBadRequest, "资产参数无效")
		return
	}
	if id := r.PathValue("id"); id != "" {
		asset.ID = id
	}
	saved, err := a.store.SaveAsset(asset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !saved.Enabled {
		for _, sessionID := range a.active.TerminateAsset(saved.ID) {
			_ = a.store.UpdateSessionStatus(sessionID, "terminating")
		}
	}
	action := "asset.create"
	if r.Method == http.MethodPut {
		action = "asset.update"
	}
	_ = a.store.AppendAudit(a.auditFor(r, action, "asset", saved.ID, true, map[string]any{"name": saved.Name, "host": saved.Host}))
	writeJSON(w, http.StatusOK, saved)
}

func (a *Application) deleteAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, sessionID := range a.active.TerminateAsset(id) {
		_ = a.store.UpdateSessionStatus(sessionID, "terminating")
	}
	if err := a.store.DeleteAsset(id); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "asset.delete", "asset", id, true, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) listCredentials(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Credentials())
}

func (a *Application) saveCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name       string         `json:"name"`
		Type       CredentialType `json:"type"`
		Secret     string         `json:"secret"`
		Passphrase string         `json:"passphrase"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "凭据参数无效")
		return
	}
	credential := Credential{ID: r.PathValue("id"), Name: input.Name, Type: input.Type}
	saved, err := a.store.SaveCredential(credential, []byte(input.Secret), []byte(input.Passphrase))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	action := "credential.create"
	if r.Method == http.MethodPut {
		action = "credential.update"
	}
	_ = a.store.AppendAudit(a.auditFor(r, action, "credential", saved.ID, true, map[string]any{"name": saved.Name, "type": saved.Type}))
	writeJSON(w, http.StatusOK, saved)
}

func (a *Application) deleteCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.DeleteCredential(id); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "credential.delete", "credential", id, true, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) listUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Users())
}

func (a *Application) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Role        Role     `json:"role"`
		AssetGroups []string `json:"asset_groups"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "用户参数无效")
		return
	}
	user, err := a.store.CreateUser(input.Username, input.Password, input.Role, input.AssetGroups...)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "user.create", "user", user.ID, true, map[string]any{"username": user.Username, "role": user.Role}))
	writeJSON(w, http.StatusCreated, user)
}

func (a *Application) updateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password    string   `json:"password"`
		Role        Role     `json:"role"`
		Enabled     bool     `json:"enabled"`
		AssetGroups []string `json:"asset_groups"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "用户参数无效")
		return
	}
	id := r.PathValue("id")
	user, err := a.store.UpdateUser(id, input.Role, input.Enabled, input.Password, input.AssetGroups)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.sessions.DeleteUser(user.ID)
	_ = a.store.AppendAudit(a.auditFor(r, "user.update", "user", user.ID, true, map[string]any{"username": user.Username, "role": user.Role, "enabled": user.Enabled}))
	writeJSON(w, http.StatusOK, user)
}

func (a *Application) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if current, ok := requestUser(r); ok && id == current.ID {
		writeAPIError(w, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}
	if err := a.store.DeleteUser(id); err != nil {
		writeStoreError(w, err)
		return
	}
	a.sessions.DeleteUser(id)
	_ = a.store.AppendAudit(a.auditFor(r, "user.delete", "user", id, true, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) listAudits(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := a.store.Audits(limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "读取审计日志失败")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *Application) listSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Sessions())
}

func (a *Application) terminateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	record, err := a.store.Session(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if record.Status != "active" || !a.active.Terminate(id) {
		writeAPIError(w, http.StatusConflict, "会话已结束或不在当前进程中")
		return
	}
	_ = a.store.UpdateSessionStatus(id, "terminating")
	_ = a.store.AppendAudit(a.auditFor(r, "session.terminate", "session", id, true, map[string]any{"asset_id": record.AssetID, "username": record.Username}))
	w.WriteHeader(http.StatusNoContent)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIError(w, http.StatusBadRequest, err.Error())
}
