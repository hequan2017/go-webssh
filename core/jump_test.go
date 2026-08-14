package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/ssh"
)

func jumpTestApp(t *testing.T) *Application {
	t.Helper()
	assetsFS := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>bastion</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	app, err := NewApplication(testConfig(t), assetsFS)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func seedCredential(t *testing.T, app *Application) string {
	t.Helper()
	credential, err := app.store.SaveCredential(Credential{Name: "jump-test", Type: CredentialPassword}, []byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return credential.ID
}

func seedAsset(t *testing.T, app *Application, name, group, jumpID, credentialID string) Asset {
	t.Helper()
	asset, err := app.store.SaveAsset(Asset{
		Name: name, Host: "10.0.0.10", Port: 22, Username: "ops", CredentialID: credentialID,
		JumpAssetID: jumpID, Group: group, Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveAsset(%s) error = %v", name, err)
	}
	return asset
}

func TestSaveAssetJumpValidation(t *testing.T) {
	app := jumpTestApp(t)
	credentialID := seedCredential(t, app)

	_, err := app.store.SaveAsset(Asset{Name: "孤儿", Host: "10.0.0.1", Port: 22, Username: "ops", CredentialID: credentialID, JumpAssetID: "missing", Group: "prod", Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "跳板机资产不存在") {
		t.Fatalf("missing jump error = %v", err)
	}

	base := seedAsset(t, app, "跳板A", "jumps", "", credentialID)

	self, err := app.store.SaveAsset(Asset{ID: base.ID, Name: base.Name, Host: base.Host, Port: 22, Username: "ops", CredentialID: credentialID, JumpAssetID: base.ID, Group: "jumps", Enabled: true})
	_ = self
	if err == nil || !strings.Contains(err.Error(), "自身") {
		t.Fatalf("self jump error = %v", err)
	}

	b := seedAsset(t, app, "目标B", "prod", base.ID, credentialID)
	if _, err := app.store.SaveAsset(Asset{ID: base.ID, Name: base.Name, Host: base.Host, Port: 22, Username: "ops", CredentialID: credentialID, JumpAssetID: b.ID, Group: "jumps", Enabled: true}); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle error = %v", err)
	}

	// 链长 MaxJumpHops 合法，再多一跳必须拒绝。
	previous := ""
	for i := 0; i < MaxJumpHops+1; i++ {
		previous = seedAsset(t, app, "链"+strings.Repeat("I", i+1), "chain", previous, credentialID).ID
	}
	if _, err := app.store.SaveAsset(Asset{Name: "超深", Host: "10.0.0.9", Port: 22, Username: "ops", CredentialID: credentialID, JumpAssetID: previous, Group: "chain", Enabled: true}); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("depth error = %v", err)
	}
}

func TestAssetSSHChainResolvesJumpOrderAndPermissions(t *testing.T) {
	app := jumpTestApp(t)
	credentialID := seedCredential(t, app)
	jump := seedAsset(t, app, "跳板A", "jumps", "", credentialID)
	target := seedAsset(t, app, "目标B", "prod", jump.ID, credentialID)

	admin := User{Role: RoleAdmin}
	chain, asset, hops, err := app.assetSSHChain(admin, target.ID)
	if err != nil {
		t.Fatalf("assetSSHChain() error = %v", err)
	}
	if len(chain) != 2 || chain[0].Host != jump.Host || chain[1].Host != target.Host {
		t.Fatalf("chain order wrong: %#v", chain)
	}
	if len(hops) != 1 || hops[0].ID != jump.ID {
		t.Fatalf("hops = %#v", hops)
	}
	if asset.ID != target.ID {
		t.Fatalf("asset = %#v", asset)
	}

	operatorNoJumpAccess := User{Role: RoleOperator, AssetGroups: []string{"prod"}}
	if _, _, _, err := app.assetSSHChain(operatorNoJumpAccess, target.ID); err == nil || !strings.Contains(err.Error(), "无权访问跳板机") {
		t.Fatalf("operator without jump access error = %v", err)
	}

	operatorFull := User{Role: RoleOperator, AssetGroups: []string{"prod", "jumps"}}
	if _, _, _, err := app.assetSSHChain(operatorFull, target.ID); err != nil {
		t.Fatalf("operator with jump access error = %v", err)
	}

	auditor := User{Role: RoleAuditor, AssetGroups: []string{"*"}}
	if _, _, _, err := app.assetSSHChain(auditor, target.ID); err == nil || !strings.Contains(err.Error(), "无权访问") {
		t.Fatalf("auditor error = %v", err)
	}
}

func TestAssetSSHChainDisabledJump(t *testing.T) {
	app := jumpTestApp(t)
	credentialID := seedCredential(t, app)
	jump := seedAsset(t, app, "跳板A", "jumps", "", credentialID)
	seedAsset(t, app, "目标B", "prod", jump.ID, credentialID)

	disabled := jump
	disabled.Enabled = false
	if _, err := app.store.SaveAsset(disabled); err != nil {
		t.Fatal(err)
	}

	admin := User{Role: RoleAdmin}
	targets := app.store.Assets()
	for _, asset := range targets {
		if asset.ID == disabled.ID {
			continue
		}
		if _, _, _, err := app.assetSSHChain(admin, asset.ID); err == nil || !strings.Contains(err.Error(), "已禁用") {
			t.Fatalf("disabled jump error = %v", err)
		}
	}
}

func TestNewSshClientChainEmpty(t *testing.T) {
	if _, err := NewSshClientChain(nil); err == nil {
		t.Fatal("empty chain should fail")
	}
}

// startTestSSHServer 启动一个接受任意密码、只接受 session 通道的最小 SSH 服务器。
func startTestSSHServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	config.AddHostKey(signer)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					_ = conn.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					switch newChan.ChannelType() {
					case "session":
						_, channelReqs, err := newChan.Accept()
						if err == nil {
							go ssh.DiscardRequests(channelReqs)
						}
					case "direct-tcpip":
						channel, channelReqs, err := newChan.Accept()
						if err != nil {
							continue
						}
						go ssh.DiscardRequests(channelReqs)
						var forward struct {
							Addr          string
							Port          uint32
							OriginAddress string
							OriginPort    uint32
						}
						if err := ssh.Unmarshal(newChan.ExtraData(), &forward); err != nil {
							_ = channel.Close()
							continue
						}
						go proxyTCPIP(channel, net.JoinHostPort(forward.Addr, strconv.Itoa(int(forward.Port))))
					default:
						_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
					}
				}
				_ = serverConn.Close()
			}()
		}
	}()
	return listener.Addr().String()
}

func TestNewSshClientChainMultiHopHandshake(t *testing.T) {
	jumpAddr := startTestSSHServer(t)
	targetAddr := startTestSSHServer(t)
	jumpHost, jumpPort, _ := net.SplitHostPort(jumpAddr)
	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	cfg := func(host, port string) *Config {
		return &Config{Host: host, Port: mustAtoi(t, port), User: "ops", Password: "secret"}
	}

	client, err := NewSshClientChain([]*Config{cfg(jumpHost, jumpPort), cfg(targetHost, targetPort)})
	if err != nil {
		t.Fatalf("multi-hop chain error = %v", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("session through jump error = %v", err)
	}
	_ = session.Close()

	if _, err := NewSshClientChain([]*Config{cfg(jumpHost, jumpPort), cfg("127.0.0.1", "1")}); err == nil || !strings.Contains(err.Error(), "经过跳板机") {
		t.Fatalf("second hop failure error = %v", err)
	}
}

// proxyTCPIP 把 direct-tcpip 通道双向桥接到真实目标地址，等价于跳板机的端口转发行为。
func proxyTCPIP(channel ssh.Channel, target string) {
	defer channel.Close()
	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(channel, upstream); done <- struct{}{} }()
	go func() { _, _ = io.Copy(upstream, channel); done <- struct{}{} }()
	<-done
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	port := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			t.Fatalf("invalid port %q", value)
		}
		port = port*10 + int(char-'0')
	}
	return port
}

func TestJumpNamesNilWhenEmpty(t *testing.T) {
	if jumpNames(nil) != nil {
		t.Fatal("jumpNames(nil) should be nil")
	}
	if got := jumpNames([]Asset{{Name: "a"}, {Name: "b"}}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("jumpNames() = %v", got)
	}
}

func TestSessionRecordJumpNamesPersisted(t *testing.T) {
	app := jumpTestApp(t)
	record := SessionRecord{ID: "s1", Username: "admin", AssetID: "a1", AssetName: "目标", JumpNames: []string{"跳板A"}, Status: "active", StartedAt: time.Now().UTC()}
	if err := app.store.AddSession(record); err != nil {
		t.Fatal(err)
	}
	saved, err := app.store.Session("s1")
	if err != nil || len(saved.JumpNames) != 1 || saved.JumpNames[0] != "跳板A" {
		t.Fatalf("session jump names = %#v, %v", saved, err)
	}
}

func TestDeleteUserRules(t *testing.T) {
	app := jumpTestApp(t)
	if _, err := app.store.CreateUser("operator3", "operator3-password-1", RoleOperator, "prod"); err != nil {
		t.Fatal(err)
	}
	users := app.store.Users()
	admin := users[0]
	var operator PublicUser
	for _, user := range users {
		if user.Username == "operator3" {
			operator = user
		}
	}
	if err := app.store.DeleteUser(admin.ID); err == nil || !strings.Contains(err.Error(), "最后一个") {
		t.Fatalf("delete last admin error = %v", err)
	}
	if err := app.store.DeleteUser(operator.ID); err != nil {
		t.Fatalf("delete operator error = %v", err)
	}
	if err := app.store.DeleteUser(operator.ID); err == nil {
		t.Fatal("delete twice should fail")
	}
}

func TestTouchLoginRecordsTime(t *testing.T) {
	app := jumpTestApp(t)
	users := app.store.Users()
	app.store.TouchLogin(users[0].ID)
	after := app.store.Users()[0]
	if after.LastLoginAt == nil || after.LastLoginAt.IsZero() {
		t.Fatal("last login time should be recorded")
	}
}
