package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("资源不存在")

type Store struct {
	mu        sync.RWMutex
	statePath string
	auditPath string
	state     bastionState
	cipher    *secretCipher
}

func NewStore(cfg *Config) (*Store, error) {
	if err := os.MkdirAll(cfg.RecordingDir(), 0o700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	secretBox, err := loadSecretCipher(cfg.DataDir, cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	s := &Store{statePath: cfg.StatePath(), auditPath: cfg.AuditPath(), cipher: secretBox, state: bastionState{Version: 1}}
	data, err := os.ReadFile(s.statePath)
	if err == nil {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("解析状态文件失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}
	if len(s.state.Users) == 0 {
		if len(cfg.AdminPassword) < 12 {
			return nil, fmt.Errorf("首次启动必须设置至少 12 位的 BASTION_ADMIN_PASSWORD")
		}
		if _, err := s.CreateUser(cfg.AdminUser, cfg.AdminPassword, RoleAdmin); err != nil {
			return nil, fmt.Errorf("创建初始管理员失败: %w", err)
		}
	}
	return s, nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath, append(data, '\n'), 0o600)
}

func (s *Store) CreateUser(username, password string, role Role, assetGroups ...string) (PublicUser, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(password) < 12 || !role.Valid() {
		return PublicUser{}, fmt.Errorf("用户名、密码或角色无效")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return PublicUser{}, err
	}
	id, err := newID()
	if err != nil {
		return PublicUser{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.state.Users {
		if strings.EqualFold(user.Username, username) {
			return PublicUser{}, fmt.Errorf("用户名已存在")
		}
	}
	user := User{ID: id, Username: username, PasswordHash: string(hash), Role: role, AssetGroups: cleanGroups(assetGroups), Enabled: true, CreatedAt: now, UpdatedAt: now}
	s.state.Users = append(s.state.Users, user)
	if err := s.saveLocked(); err != nil {
		return PublicUser{}, err
	}
	return user.Public(), nil
}

func (s *Store) Authenticate(username, password string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.state.Users {
		if strings.EqualFold(user.Username, strings.TrimSpace(username)) && user.Enabled {
			if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
				return user, nil
			}
			break
		}
	}
	return User{}, fmt.Errorf("用户名或密码错误")
}

func (s *Store) User(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.state.Users {
		if user.ID == id {
			return user, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *Store) Users() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]PublicUser, 0, len(s.state.Users))
	for _, user := range s.state.Users {
		users = append(users, user.Public())
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users
}

func (s *Store) UpdateUser(id string, role Role, enabled bool, password string, assetGroups []string) (PublicUser, error) {
	if !role.Valid() {
		return PublicUser{}, fmt.Errorf("角色无效")
	}
	var hash []byte
	var err error
	if password != "" {
		if len(password) < 12 {
			return PublicUser{}, fmt.Errorf("密码不能少于 12 位")
		}
		hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return PublicUser{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Users {
		if s.state.Users[i].ID == id {
			if s.state.Users[i].Role == RoleAdmin && s.state.Users[i].Enabled && (role != RoleAdmin || !enabled) {
				enabledAdmins := 0
				for _, user := range s.state.Users {
					if user.Role == RoleAdmin && user.Enabled {
						enabledAdmins++
					}
				}
				if enabledAdmins <= 1 {
					return PublicUser{}, fmt.Errorf("不能禁用或降级最后一个管理员")
				}
			}
			s.state.Users[i].Role = role
			s.state.Users[i].AssetGroups = cleanGroups(assetGroups)
			s.state.Users[i].Enabled = enabled
			s.state.Users[i].UpdatedAt = time.Now().UTC()
			if len(hash) > 0 {
				s.state.Users[i].PasswordHash = string(hash)
			}
			if err := s.saveLocked(); err != nil {
				return PublicUser{}, err
			}
			return s.state.Users[i].Public(), nil
		}
	}
	return PublicUser{}, ErrNotFound
}

func cleanGroups(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	clean := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		clean = append(clean, group)
	}
	sort.Strings(clean)
	return clean
}

func validateAsset(asset Asset) error {
	if strings.TrimSpace(asset.Name) == "" || strings.TrimSpace(asset.Host) == "" || strings.TrimSpace(asset.Username) == "" || strings.TrimSpace(asset.Group) == "" {
		return fmt.Errorf("资产名称、主机、用户名和资产组不能为空")
	}
	if asset.Port < 1 || asset.Port > 65535 {
		return fmt.Errorf("SSH 端口无效")
	}
	if asset.HostKeyFingerprint != "" && !strings.HasPrefix(asset.HostKeyFingerprint, "SHA256:") {
		return fmt.Errorf("SSH 主机密钥指纹必须使用 SHA256 格式")
	}
	if strings.TrimSpace(asset.CredentialID) == "" {
		return fmt.Errorf("资产必须关联登录凭据")
	}
	return nil
}

func (s *Store) SaveAsset(asset Asset) (Asset, error) {
	asset.Name = strings.TrimSpace(asset.Name)
	asset.Host = strings.TrimSpace(asset.Host)
	asset.Username = strings.TrimSpace(asset.Username)
	asset.Group = strings.TrimSpace(asset.Group)
	asset.HostKeyFingerprint = strings.TrimSpace(asset.HostKeyFingerprint)
	if err := validateAsset(asset); err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if asset.CredentialID != "" && !s.credentialExistsLocked(asset.CredentialID) {
		return Asset{}, fmt.Errorf("凭据不存在")
	}
	if asset.ID == "" {
		var err error
		asset.ID, err = newID()
		if err != nil {
			return Asset{}, err
		}
		asset.CreatedAt = now
		asset.UpdatedAt = now
		s.state.Assets = append(s.state.Assets, asset)
	} else {
		found := false
		for i := range s.state.Assets {
			if s.state.Assets[i].ID == asset.ID {
				asset.CreatedAt = s.state.Assets[i].CreatedAt
				asset.UpdatedAt = now
				s.state.Assets[i] = asset
				found = true
				break
			}
		}
		if !found {
			return Asset{}, ErrNotFound
		}
	}
	return asset, s.saveLocked()
}

func (s *Store) Assets() []Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	assets := append(make([]Asset, 0, len(s.state.Assets)), s.state.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return assets
}

func (s *Store) Asset(id string) (Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, asset := range s.state.Assets {
		if asset.ID == id {
			return asset, nil
		}
	}
	return Asset{}, ErrNotFound
}

func (s *Store) DeleteAsset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Assets {
		if s.state.Assets[i].ID == id {
			s.state.Assets = append(s.state.Assets[:i], s.state.Assets[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) SaveCredential(credential Credential, secret, passphrase []byte) (PublicCredential, error) {
	if strings.TrimSpace(credential.Name) == "" || !credential.Type.Valid() {
		return PublicCredential{}, fmt.Errorf("凭据名称或类型无效")
	}
	if len(secret) == 0 && credential.ID == "" {
		return PublicCredential{}, fmt.Errorf("凭据内容不能为空")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(secret) > 0 {
		encrypted, err := s.cipher.Encrypt(secret)
		if err != nil {
			return PublicCredential{}, err
		}
		credential.EncryptedSecret = encrypted
	}
	if len(passphrase) > 0 {
		encrypted, err := s.cipher.Encrypt(passphrase)
		if err != nil {
			return PublicCredential{}, err
		}
		credential.EncryptedPassphrase = encrypted
	}
	if credential.ID == "" {
		var err error
		credential.ID, err = newID()
		if err != nil {
			return PublicCredential{}, err
		}
		credential.CreatedAt = now
		credential.UpdatedAt = now
		s.state.Credentials = append(s.state.Credentials, credential)
	} else {
		found := false
		for i := range s.state.Credentials {
			if s.state.Credentials[i].ID == credential.ID {
				if len(secret) == 0 && credential.Type != s.state.Credentials[i].Type {
					return PublicCredential{}, fmt.Errorf("修改凭据类型时必须提供新凭据内容")
				}
				credential.CreatedAt = s.state.Credentials[i].CreatedAt
				credential.UpdatedAt = now
				if credential.EncryptedSecret == "" {
					credential.EncryptedSecret = s.state.Credentials[i].EncryptedSecret
				}
				if credential.EncryptedPassphrase == "" {
					credential.EncryptedPassphrase = s.state.Credentials[i].EncryptedPassphrase
				}
				s.state.Credentials[i] = credential
				found = true
				break
			}
		}
		if !found {
			return PublicCredential{}, ErrNotFound
		}
	}
	if err := s.saveLocked(); err != nil {
		return PublicCredential{}, err
	}
	return credential.Public(), nil
}

func (s *Store) Credentials() []PublicCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	credentials := make([]PublicCredential, 0, len(s.state.Credentials))
	for _, credential := range s.state.Credentials {
		credentials = append(credentials, credential.Public())
	}
	sort.Slice(credentials, func(i, j int) bool { return credentials[i].Name < credentials[j].Name })
	return credentials
}

func (s *Store) CredentialSecret(id string) (Credential, []byte, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, credential := range s.state.Credentials {
		if credential.ID == id {
			secret, err := s.cipher.Decrypt(credential.EncryptedSecret)
			if err != nil {
				return Credential{}, nil, nil, err
			}
			var passphrase []byte
			if credential.EncryptedPassphrase != "" {
				passphrase, err = s.cipher.Decrypt(credential.EncryptedPassphrase)
			}
			return credential, secret, passphrase, err
		}
	}
	return Credential{}, nil, nil, ErrNotFound
}

func (s *Store) credentialExistsLocked(id string) bool {
	for _, credential := range s.state.Credentials {
		if credential.ID == id {
			return true
		}
	}
	return false
}

func (s *Store) DeleteCredential(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, asset := range s.state.Assets {
		if asset.CredentialID == id {
			return fmt.Errorf("凭据正被资产使用")
		}
	}
	for i := range s.state.Credentials {
		if s.state.Credentials[i].ID == id {
			s.state.Credentials = append(s.state.Credentials[:i], s.state.Credentials[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) AddSession(record SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Sessions = append(s.state.Sessions, record)
	return s.saveLocked()
}

func (s *Store) EndSession(id, status string) error {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Sessions {
		if s.state.Sessions[i].ID == id {
			s.state.Sessions[i].Status = status
			s.state.Sessions[i].EndedAt = &now
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) UpdateSessionStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Sessions {
		if s.state.Sessions[i].ID == id {
			s.state.Sessions[i].Status = status
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) Session(id string) (SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, record := range s.state.Sessions {
		if record.ID == id {
			return record, nil
		}
	}
	return SessionRecord{}, ErrNotFound
}

func (s *Store) Sessions() []SessionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := append(make([]SessionRecord, 0, len(s.state.Sessions)), s.state.Sessions...)
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.After(records[j].StartedAt) })
	if len(records) > 500 {
		records = records[:500]
	}
	return records
}

func (s *Store) AppendAudit(event AuditEvent) error {
	if event.ID == "" {
		var err error
		event.ID, err = newID()
		if err != nil {
			return err
		}
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func (s *Store) Audits(limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	s.mu.RLock()
	data, err := os.ReadFile(s.auditPath)
	s.mu.RUnlock()
	if os.IsNotExist(err) {
		return []AuditEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	start := len(lines) - limit
	if start < 0 {
		start = 0
	}
	events := make([]AuditEvent, 0, len(lines)-start)
	for i := len(lines) - 1; i >= start; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var event AuditEvent
		if json.Unmarshal([]byte(lines[i]), &event) == nil {
			events = append(events, event)
		}
	}
	return events, nil
}
