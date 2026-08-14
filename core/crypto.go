package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type secretCipher struct {
	aead cipher.AEAD
}

func loadSecretCipher(dataDir string, configuredKey []byte) (*secretCipher, error) {
	key := configuredKey
	if len(key) == 0 {
		keyPath := filepath.Join(dataDir, "master.key")
		encoded, err := os.ReadFile(keyPath)
		if err == nil {
			key, err = base64.StdEncoding.DecodeString(string(encoded))
			if err != nil {
				return nil, fmt.Errorf("解析主密钥文件失败: %w", err)
			}
		} else if os.IsNotExist(err) {
			key = make([]byte, 32)
			if _, err := io.ReadFull(rand.Reader, key); err != nil {
				return nil, fmt.Errorf("生成主密钥失败: %w", err)
			}
			if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
				return nil, fmt.Errorf("保存主密钥失败: %w", err)
			}
		} else {
			return nil, fmt.Errorf("读取主密钥失败: %w", err)
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("主密钥必须是 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretCipher{aead: aead}, nil
}

func (c *secretCipher) Encrypt(plain []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *secretCipher) Decrypt(encoded string) ([]byte, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize {
		return nil, fmt.Errorf("加密数据长度无效")
	}
	return c.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
}
