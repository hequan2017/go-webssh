package core

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// Config 描述 Web 服务及目标 SSH 主机配置。
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	KeyPath  string
	Addr     string
}

// LoadConfig 从环境变量加载配置。
func LoadConfig() *Config {
	return &Config{
		Host:     getEnv("SSH_HOST", "127.0.0.1"),
		Port:     getEnvInt("SSH_PORT", 22),
		User:     getEnv("SSH_USER", "root"),
		Password: os.Getenv("SSH_PASSWORD"),
		KeyPath:  os.Getenv("SSH_KEY_PATH"),
		Addr:     getEnv("LISTEN_ADDR", ":8080"),
	}
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
	return nil
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
