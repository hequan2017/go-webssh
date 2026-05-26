package core

import (
	"fmt"
	"os"
	"strconv"
)

// AppConfig 全局配置，由 main 初始化
var AppConfig *Config

// Config SSH 连接配置
type Config struct {
	Host     string // SSH 服务器地址
	Port     int    // SSH 端口
	User     string // 用户名
	Password string // 密码
	KeyPath  string // 密钥文件路径（可选，优先于密码）
	Addr     string // 监听地址，格式 host:port
}

// LoadConfig 从环境变量加载配置，未设置的使用默认值
func LoadConfig() *Config {
	cfg := &Config{
		Host:     getEnv("SSH_HOST", "127.0.0.1"),
		Port:     getEnvInt("SSH_PORT", 22),
		User:     getEnv("SSH_USER", "root"),
		Password: getEnv("SSH_PASSWORD", ""),
		KeyPath:  getEnv("SSH_KEY_PATH", ""),
		Addr:     getEnv("LISTEN_ADDR", ":8080"),
	}
	return cfg
}

// Address 返回 SSH 服务器地址，格式 host:port
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
