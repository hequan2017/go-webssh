package core

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config 描述 Web 服务及目标 SSH 主机配置。
type Config struct {
	Host               string
	Port               int
	User               string
	Password           string
	KeyPath            string
	KeyData            []byte
	KeyPassphrase      []byte
	HostKeyFingerprint string
	Addr               string
	DataDir            string
	MasterKey          []byte
	AdminUser          string
	AdminPassword      string
	SessionTTL         time.Duration
	MaxUploadBytes     int64
}

// LoadConfig 从环境变量加载配置。
func LoadConfig() *Config {
	cfg := &Config{
		Host:           getEnv("SSH_HOST", "127.0.0.1"),
		Port:           getEnvInt("SSH_PORT", 22),
		User:           getEnv("SSH_USER", "root"),
		Password:       os.Getenv("SSH_PASSWORD"),
		KeyPath:        os.Getenv("SSH_KEY_PATH"),
		Addr:           getEnv("LISTEN_ADDR", ":8080"),
		DataDir:        getEnv("BASTION_DATA_DIR", "data"),
		AdminUser:      getEnv("BASTION_ADMIN_USER", "admin"),
		AdminPassword:  os.Getenv("BASTION_ADMIN_PASSWORD"),
		SessionTTL:     time.Duration(getEnvInt("BASTION_SESSION_HOURS", 12)) * time.Hour,
		MaxUploadBytes: int64(getEnvInt("BASTION_MAX_UPLOAD_MB", 100)) << 20,
	}
	if encoded := os.Getenv("BASTION_MASTER_KEY"); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			cfg.MasterKey = []byte{0}
		} else {
			cfg.MasterKey = decoded
		}
	}
	return cfg
}

func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("SSH_HOST 不能为空")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("SSH_PORT 必须在 1 到 65535 之间")
	}
	if c.User == "" {
		return fmt.Errorf("SSH_USER 不能为空")
	}
	if c.Addr == "" {
		return fmt.Errorf("LISTEN_ADDR 不能为空")
	}
	if c.DataDir == "" {
		return fmt.Errorf("BASTION_DATA_DIR 不能为空")
	}
	if c.SessionTTL < time.Hour {
		return fmt.Errorf("BASTION_SESSION_HOURS 不能小于 1")
	}
	if c.MaxUploadBytes < 1<<20 {
		return fmt.Errorf("BASTION_MAX_UPLOAD_MB 不能小于 1")
	}
	if len(c.MasterKey) != 0 && len(c.MasterKey) != 32 {
		return fmt.Errorf("BASTION_MASTER_KEY 必须是 Base64 编码的 32 字节密钥")
	}
	return nil
}

func (c *Config) StatePath() string {
	return filepath.Join(c.DataDir, "state.json")
}

func (c *Config) AuditPath() string {
	return filepath.Join(c.DataDir, "audit.jsonl")
}

func (c *Config) RecordingDir() string {
	return filepath.Join(c.DataDir, "recordings")
}

func (c *Config) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c *Config) Target() string {
	return fmt.Sprintf("%s@%s", c.User, c.Address())
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
