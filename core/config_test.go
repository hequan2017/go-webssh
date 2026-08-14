package core

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// 清理可能存在的环境变量
	os.Unsetenv("SSH_HOST")
	os.Unsetenv("SSH_PORT")
	os.Unsetenv("SSH_USER")
	os.Unsetenv("SSH_PASSWORD")
	os.Unsetenv("SSH_KEY_PATH")
	os.Unsetenv("LISTEN_ADDR")

	cfg := LoadConfig()

	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 22 {
		t.Errorf("Port = %d, want %d", cfg.Port, 22)
	}
	if cfg.User != "root" {
		t.Errorf("User = %q, want %q", cfg.User, "root")
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
	if cfg.KeyPath != "" {
		t.Errorf("KeyPath = %q, want empty", cfg.KeyPath)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	os.Setenv("SSH_HOST", "192.168.1.100")
	os.Setenv("SSH_PORT", "2222")
	os.Setenv("SSH_USER", "admin")
	os.Setenv("SSH_PASSWORD", "secret")
	os.Setenv("LISTEN_ADDR", ":9090")
	defer func() {
		os.Unsetenv("SSH_HOST")
		os.Unsetenv("SSH_PORT")
		os.Unsetenv("SSH_USER")
		os.Unsetenv("SSH_PASSWORD")
		os.Unsetenv("LISTEN_ADDR")
	}()

	cfg := LoadConfig()

	if cfg.Host != "192.168.1.100" {
		t.Errorf("Host = %q, want %q", cfg.Host, "192.168.1.100")
	}
	if cfg.Port != 2222 {
		t.Errorf("Port = %d, want %d", cfg.Port, 2222)
	}
	if cfg.User != "admin" {
		t.Errorf("User = %q, want %q", cfg.User, "admin")
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want %q", cfg.Password, "secret")
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9090")
	}
}

func TestConfig_Address(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"default", &Config{Host: "127.0.0.1", Port: 22}, "127.0.0.1:22"},
		{"custom port", &Config{Host: "10.0.0.1", Port: 2222}, "10.0.0.1:2222"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Address(); got != tt.want {
				t.Errorf("Address() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfig_AddressIPv6(t *testing.T) {
	cfg := &Config{Host: "::1", Port: 22}
	if got := cfg.Address(); got != "[::1]:22" {
		t.Fatalf("Address() = %q, want %q", got, "[::1]:22")
	}
}

func TestConfigValidate(t *testing.T) {
	valid := &Config{Host: "127.0.0.1", Port: 22, User: "root", Addr: ":8080", DataDir: "data", SessionTTL: 12 * time.Hour, MaxUploadBytes: 100 << 20}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	invalid := *valid
	invalid.Port = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() should reject port 0")
	}
}

func TestGetEnv_Fallback(t *testing.T) {
	os.Unsetenv("_TEST_WEBSSH_X")
	if got := getEnv("_TEST_WEBSSH_X", "fallback"); got != "fallback" {
		t.Errorf("getEnv() = %q, want %q", got, "fallback")
	}
}

func TestGetEnv_Set(t *testing.T) {
	os.Setenv("_TEST_WEBSSH_X", "value")
	defer os.Unsetenv("_TEST_WEBSSH_X")
	if got := getEnv("_TEST_WEBSSH_X", "fallback"); got != "value" {
		t.Errorf("getEnv() = %q, want %q", got, "value")
	}
}

func TestGetEnvInt_Fallback(t *testing.T) {
	os.Unsetenv("_TEST_WEBSSH_INT")
	if got := getEnvInt("_TEST_WEBSSH_INT", 42); got != 42 {
		t.Errorf("getEnvInt() = %d, want %d", got, 42)
	}
}

func TestGetEnvInt_Set(t *testing.T) {
	os.Setenv("_TEST_WEBSSH_INT", "8080")
	defer os.Unsetenv("_TEST_WEBSSH_INT")
	if got := getEnvInt("_TEST_WEBSSH_INT", 42); got != 8080 {
		t.Errorf("getEnvInt() = %d, want %d", got, 8080)
	}
}

func TestGetEnvInt_Invalid(t *testing.T) {
	os.Setenv("_TEST_WEBSSH_INT", "not_a_number")
	defer os.Unsetenv("_TEST_WEBSSH_INT")
	if got := getEnvInt("_TEST_WEBSSH_INT", 42); got != 42 {
		t.Errorf("getEnvInt() with invalid value = %d, want fallback %d", got, 42)
	}
}
