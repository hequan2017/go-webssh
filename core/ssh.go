package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func NewSshClient(cfg *Config) (*ssh.Client, error) {
	auth, err := buildAuthMethod(cfg)
	if err != nil {
		return nil, err
	}

	client, err := ssh.Dial("tcp", cfg.Address(), &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 兼容原项目行为，生产环境建议放在可信网络或反向代理之后。
		Timeout:         8 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 SSH 主机 %s 失败: %w", cfg.Address(), err)
	}
	return client, nil
}

func buildAuthMethod(cfg *Config) ([]ssh.AuthMethod, error) {
	if cfg.KeyPath != "" {
		signer, err := publicKeySigner(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("加载 SSH 私钥失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("必须设置 SSH_PASSWORD 或 SSH_KEY_PATH")
	}
	return []ssh.AuthMethod{ssh.Password(cfg.Password)}, nil
}

func publicKeySigner(path string) (ssh.Signer, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("读取私钥 %q 失败: %w", expanded, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("解析私钥 %q 失败: %w", expanded, err)
	}
	return signer, nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
