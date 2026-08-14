package core

import (
	"os"
	"testing"
)

func TestBuildAuthMethod_Password(t *testing.T) {
	cfg := &Config{
		User:     "root",
		Password: "secret",
	}
	auth, err := buildAuthMethod(cfg)
	if err != nil {
		t.Fatalf("buildAuthMethod() error = %v", err)
	}
	// 密码 + 键盘交互回退，兼容只接受 keyboard-interactive 的设备
	if len(auth) != 2 {
		t.Fatalf("len(auth) = %d, want 2", len(auth))
	}
}

func TestBuildAuthMethod_Empty(t *testing.T) {
	cfg := &Config{}
	_, err := buildAuthMethod(cfg)
	if err == nil {
		t.Fatal("expected error when both password and key path are empty")
	}
}

func TestBuildAuthMethod_KeyPathNotFound(t *testing.T) {
	cfg := &Config{
		KeyPath: "/nonexistent/path/to/key",
	}
	_, err := buildAuthMethod(cfg)
	if err == nil {
		t.Fatal("expected error for non-existent key file")
	}
}

func TestBuildAuthMethod_KeyPathTakesPrecedence(t *testing.T) {
	cfg := &Config{
		Password: "secret",
		KeyPath:  "/nonexistent/key",
	}
	// KeyPath 优先，即使 password 有值也会走 key 路径并报错
	_, err := buildAuthMethod(cfg)
	if err == nil {
		t.Fatal("expected error for non-existent key file (key should take precedence)")
	}
}

func TestNewSshClient_InvalidHost(t *testing.T) {
	cfg := &Config{
		Host:     "127.0.0.1",
		Port:     29999, // 不可能开放的端口
		User:     "root",
		Password: "test",
	}
	_, err := NewSshClient(cfg)
	if err == nil {
		t.Fatal("expected error connecting to invalid host")
	}
}

func TestPublicKeySigner_InvalidPath(t *testing.T) {
	_, err := publicKeySigner("/nonexistent/id_rsa")
	if err == nil {
		t.Fatal("expected error for non-existent key")
	}
}

func TestPublicKeySigner_InvalidKey(t *testing.T) {
	// 创建一个临时文件，内容不是有效的密钥
	f, err := os.CreateTemp("", "test_key_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("not a valid private key")
	f.Close()

	_, err = publicKeySigner(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid key content")
	}
}
