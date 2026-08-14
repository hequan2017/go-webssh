package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

type activeSessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]activeSession
}

type activeSession struct {
	AssetID string
	Cancel  context.CancelFunc
}

func newActiveSessionRegistry() *activeSessionRegistry {
	return &activeSessionRegistry{sessions: make(map[string]activeSession)}
}

func (r *activeSessionRegistry) Add(id, assetID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = activeSession{AssetID: assetID, Cancel: cancel}
}

func (r *activeSessionRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

func (r *activeSessionRegistry) Terminate(id string) bool {
	r.mu.Lock()
	session, ok := r.sessions[id]
	r.mu.Unlock()
	if ok {
		session.Cancel()
	}
	return ok
}

func (r *activeSessionRegistry) TerminateAsset(assetID string) []string {
	r.mu.Lock()
	matched := make(map[string]context.CancelFunc)
	for id, session := range r.sessions {
		if session.AssetID == assetID {
			matched[id] = session.Cancel
		}
	}
	r.mu.Unlock()
	ids := make([]string, 0, len(matched))
	for id, cancel := range matched {
		ids = append(ids, id)
		cancel()
	}
	return ids
}

const sessionCookieName = "bastion_session"

type authSession struct {
	UserID    string
	ExpiresAt time.Time
}

type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]authSession
	ttl      time.Duration
}

type loginAttempt struct {
	Failures int
	ResetAt  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if !attempt.ResetAt.After(time.Now()) {
		delete(l.attempts, key)
		return true
	}
	return attempt.Failures < 5
}

func (l *loginLimiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if !attempt.ResetAt.After(time.Now()) {
		attempt = loginAttempt{ResetAt: time.Now().Add(5 * time.Minute)}
	}
	attempt.Failures++
	l.attempts[key] = attempt
}

func (l *loginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func newSessionManager(ttl time.Duration) *sessionManager {
	return &sessionManager{sessions: make(map[string]authSession), ttl: ttl}
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (m *sessionManager) Create(userID string) (string, time.Time, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	expires := time.Now().Add(m.ttl)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeExpiredLocked(time.Now())
	m.sessions[sessionKey(token)] = authSession{UserID: userID, ExpiresAt: expires}
	return token, expires, nil
}

func (m *sessionManager) Resolve(token string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.removeExpiredLocked(now)
	session, ok := m.sessions[sessionKey(token)]
	if !ok || !session.ExpiresAt.After(now) {
		return "", false
	}
	return session.UserID, true
}

func (m *sessionManager) Delete(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionKey(token))
}

func (m *sessionManager) DeleteUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, session := range m.sessions {
		if session.UserID == userID {
			delete(m.sessions, key)
		}
	}
}

func (m *sessionManager) removeExpiredLocked(now time.Time) {
	for key, session := range m.sessions {
		if !session.ExpiresAt.After(now) {
			delete(m.sessions, key)
		}
	}
}

type userContextKey struct{}

func withUser(r *http.Request, user User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContextKey{}, user))
}

func requestUser(r *http.Request) (User, bool) {
	user, ok := r.Context().Value(userContextKey{}).(User)
	return user, ok
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func hasRole(user User, roles ...Role) bool {
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	return false
}

func canAccessAsset(user User, asset Asset) bool {
	if user.Role == RoleAdmin {
		return true
	}
	if user.Role != RoleOperator {
		return false
	}
	for _, group := range user.AssetGroups {
		if group == "*" || group == asset.Group {
			return true
		}
	}
	return false
}
