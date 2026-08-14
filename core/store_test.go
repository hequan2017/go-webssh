package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Host: "127.0.0.1", Port: 22, User: "root", Addr: ":8080",
		DataDir: t.TempDir(), AdminUser: "admin", AdminPassword: "very-secure-password",
		SessionTTL: 12 * time.Hour, MaxUploadBytes: 10 << 20,
	}
}

func TestStoreEncryptsCredentialAndAuthenticatesUser(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.Authenticate("admin", cfg.AdminPassword)
	if err != nil || admin.Role != RoleAdmin {
		t.Fatalf("Authenticate() = %#v, %v", admin, err)
	}

	credential, err := store.SaveCredential(Credential{Name: "root-key", Type: CredentialPrivateKey}, []byte("private-key-content"), []byte("key-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(state) == "" || bytes.Contains(state, []byte("private-key-content")) || bytes.Contains(state, []byte("key-passphrase")) {
		t.Fatal("状态文件不应包含明文凭据")
	}
	_, secret, passphrase, err := store.CredentialSecret(credential.ID)
	if err != nil || string(secret) != "private-key-content" || string(passphrase) != "key-passphrase" {
		t.Fatalf("CredentialSecret() = %q, %v", secret, err)
	}
}

func TestStoreProtectsLastEnabledAdmin(t *testing.T) {
	store, err := NewStore(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	admin := store.Users()[0]
	if _, err := store.UpdateUser(admin.ID, RoleAuditor, true, "", nil); err == nil {
		t.Fatal("last enabled admin should not be downgraded")
	}
	if _, err := store.UpdateUser(admin.ID, RoleAdmin, false, "", nil); err == nil {
		t.Fatal("last enabled admin should not be disabled")
	}
}

func TestAssetGroupAuthorization(t *testing.T) {
	operator := User{Role: RoleOperator, AssetGroups: []string{"prod"}}
	if !canAccessAsset(operator, Asset{Group: "prod"}) {
		t.Fatal("operator should access assigned group")
	}
	if canAccessAsset(operator, Asset{Group: "test"}) {
		t.Fatal("operator should not access unassigned group")
	}
	if !canAccessAsset(User{Role: RoleAdmin}, Asset{Group: "test"}) {
		t.Fatal("admin should access every group")
	}
}
