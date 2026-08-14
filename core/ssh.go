package core

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// NewSshClient 建立单跳 SSH 连接。
func NewSshClient(cfg *Config) (*ssh.Client, error) {
	return NewSshClientChain([]*Config{cfg})
}

// NewSshClientChain 按顺序建立多跳 SSH 连接：chain[0] 直连，其余各跳通过上一跳
// 的转发通道拨号（等价于 ssh 的 ProxyJump）。任何一跳失败都会释放已建立的连接。
func NewSshClientChain(chain []*Config) (*ssh.Client, error) {
	if len(chain) == 0 {
		return nil, fmt.Errorf("SSH 连接配置为空")
	}
	var client *ssh.Client
	for i, cfg := range chain {
		next, err := dialSSH(cfg, client)
		if err != nil {
			if client != nil {
				_ = client.Close()
			}
			if i > 0 {
				return nil, fmt.Errorf("经过跳板机连接 %s 失败: %w", cfg.Address(), err)
			}
			return nil, fmt.Errorf("连接 SSH 主机 %s 失败: %w", cfg.Address(), err)
		}
		client = next
	}
	return client, nil
}

func dialSSH(cfg *Config, via *ssh.Client) (*ssh.Client, error) {
	auth, err := buildAuthMethod(cfg)
	if err != nil {
		return nil, err
	}

	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if cfg.HostKeyFingerprint != "" {
		hostKeyCallback = func(_ string, _ net.Addr, key ssh.PublicKey) error {
			actual := ssh.FingerprintSHA256(key)
			if actual != cfg.HostKeyFingerprint {
				return fmt.Errorf("SSH 主机密钥不匹配: 期望 %s，实际 %s", cfg.HostKeyFingerprint, actual)
			}
			return nil
		}
	}
	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         8 * time.Second,
	}
	if via == nil {
		client, err := ssh.Dial("tcp", cfg.Address(), config)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	conn, err := via.Dial("tcp", cfg.Address())
	if err != nil {
		return nil, err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, cfg.Address(), config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func buildAuthMethod(cfg *Config) ([]ssh.AuthMethod, error) {
	if len(cfg.KeyData) > 0 {
		var signer ssh.Signer
		var err error
		if len(cfg.KeyPassphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(cfg.KeyData, cfg.KeyPassphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(cfg.KeyData)
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
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
	// 部分设备（交换机、加固系统等）只接受键盘交互认证，密码一致时自动用于回答交互提示。
	return []ssh.AuthMethod{
		ssh.Password(cfg.Password),
		ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = cfg.Password
			}
			return answers, nil
		}),
	}, nil
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
