package core

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleAuditor  Role = "auditor"
)

func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleOperator || r == RoleAuditor
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Role         Role      `json:"role"`
	AssetGroups  []string  `json:"asset_groups,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type PublicUser struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Role        Role      `json:"role"`
	AssetGroups []string  `json:"asset_groups,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (u User) Public() PublicUser {
	return PublicUser{ID: u.ID, Username: u.Username, Role: u.Role, AssetGroups: append([]string(nil), u.AssetGroups...), Enabled: u.Enabled, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

// MaxJumpHops 限定单个资产可级联的跳板机数量上限。
const MaxJumpHops = 5

type Asset struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	Username           string    `json:"username"`
	CredentialID       string    `json:"credential_id"`
	JumpAssetID        string    `json:"jump_asset_id,omitempty"`
	Group              string    `json:"group"`
	Description        string    `json:"description"`
	HostKeyFingerprint string    `json:"host_key_fingerprint"`
	Enabled            bool      `json:"enabled"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CredentialType string

const (
	CredentialPassword   CredentialType = "password"
	CredentialPrivateKey CredentialType = "private_key"
)

func (t CredentialType) Valid() bool {
	return t == CredentialPassword || t == CredentialPrivateKey
}

type Credential struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Type                CredentialType `json:"type"`
	EncryptedSecret     string         `json:"encrypted_secret,omitempty"`
	EncryptedPassphrase string         `json:"encrypted_passphrase,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type PublicCredential struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      CredentialType `json:"type"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (c Credential) Public() PublicCredential {
	return PublicCredential{ID: c.ID, Name: c.Name, Type: c.Type, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

type SessionRecord struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	Username      string     `json:"username"`
	AssetID       string     `json:"asset_id"`
	AssetName     string     `json:"asset_name"`
	JumpNames     []string   `json:"jump_names,omitempty"`
	ClientIP      string     `json:"client_ip"`
	Status        string     `json:"status"`
	RecordingPath string     `json:"recording_path,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

type AuditEvent struct {
	ID           string         `json:"id"`
	Time         time.Time      `json:"time"`
	UserID       string         `json:"user_id,omitempty"`
	Username     string         `json:"username,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	ClientIP     string         `json:"client_ip,omitempty"`
	Success      bool           `json:"success"`
	Detail       map[string]any `json:"detail,omitempty"`
}

type bastionState struct {
	Version     int             `json:"version"`
	Users       []User          `json:"users"`
	Assets      []Asset         `json:"assets"`
	Credentials []Credential    `json:"credentials"`
	Sessions    []SessionRecord `json:"sessions"`
}
