package core

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/crypto/ssh"
)

// NewSshClient 根据配置创建 SSH 客户端连接
func NewSshClient(cfg *Config) (*ssh.Client, error) {
	auth, err := buildAuthMethod(cfg)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		Timeout:         time.Second * 5,
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", cfg.Address(), config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s failed: %w", cfg.Address(), err)
	}
	return client, nil
}

// buildAuthMethod 根据 Config 构建认证方式，密钥优先
func buildAuthMethod(cfg *Config) ([]ssh.AuthMethod, error) {
	if cfg.KeyPath != "" {
		signer, err := publicKeySigner(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("load key %s failed: %w", cfg.KeyPath, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("SSH_PASSWORD 或 SSH_KEY_PATH 必须设置其中一个")
	}
	return []ssh.AuthMethod{ssh.Password(cfg.Password)}, nil
}

// publicKeySigner 从密钥文件创建 SSH signer
func publicKeySigner(kPath string) (ssh.Signer, error) {
	keyPath, err := homedir.Expand(kPath)
	if err != nil {
		return nil, fmt.Errorf("expand key path failed: %w", err)
	}
	key, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key file failed: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse private key failed: %w", err)
	}
	return signer, nil
}

// hostKeyCallback 基于 known_hosts 的安全主机密钥验证（可选）
func hostKeyCallback(host string) ssh.HostKeyCallback {
	hostPath, err := homedir.Expand("~/.ssh/known_hosts")
	if err != nil {
		log.Fatalf("find known_hosts home dir failed: %v", err)
	}
	file, err := os.Open(hostPath)
	if err != nil {
		log.Fatalf("open known_hosts failed: %v", err)
	}
	defer file.Close()

	var hostKey ssh.PublicKey
	for buf := make([]byte, 4096); ; {
		n, err := file.Read(buf)
		if n == 0 {
			break
		}
		_ = err
		hostKey, _, _, _, _ = ssh.ParseAuthorizedKey(buf[:n])
	}
	if hostKey == nil {
		log.Fatalf("no hostkey found for %s", host)
	}
	return ssh.FixedHostKey(hostKey)
}
