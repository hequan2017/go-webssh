package core

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExpandCIDR(t *testing.T) {
	hosts, err := expandCIDR("192.168.112.0/29")
	if err != nil {
		t.Fatal(err)
	}
	// /29 共 8 个地址，去掉网络地址和广播地址剩 6 个
	if len(hosts) != 6 || hosts[0] != "192.168.112.1" || hosts[5] != "192.168.112.6" {
		t.Fatalf("hosts = %v", hosts)
	}

	single, err := expandCIDR("192.168.112.36")
	if err != nil || len(single) != 1 || single[0] != "192.168.112.36" {
		t.Fatalf("single host = %v, %v", single, err)
	}

	full, err := expandCIDR("10.0.0.3/32")
	if err != nil || len(full) != 1 || full[0] != "10.0.0.3" {
		t.Fatalf("single cidr = %v, %v", full, err)
	}

	if _, err := expandCIDR("not-a-cidr"); err == nil || !strings.Contains(err.Error(), "网段格式无效") {
		t.Fatalf("invalid cidr error = %v", err)
	}
	if _, err := expandCIDR(""); err == nil {
		t.Fatal("empty cidr should fail")
	}
	if _, err := expandCIDR("10.0.0.0/8"); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("oversized cidr error = %v", err)
	}
}

func TestProbeSSHDetectsServerBanner(t *testing.T) {
	addr := startTestSSHServer(t)
	host, port, _ := splitHostPort(t, addr)
	result, ok := probeSSH(context.Background(), host, port, 2*time.Second)
	if !ok || !result.IsSSH {
		t.Fatalf("probeSSH() = %#v, ok=%v", result, ok)
	}
	if !strings.HasPrefix(result.SSHBanner, "SSH-") {
		t.Fatalf("banner = %q", result.SSHBanner)
	}
}

func TestScanHostsFindsOnlyOpenPorts(t *testing.T) {
	addr := startTestSSHServer(t)
	host, port, _ := splitHostPort(t, addr)
	found := scanHosts(context.Background(), []string{host}, port, 500*time.Millisecond)
	if len(found) != 1 || found[0].Host != host || !found[0].IsSSH {
		t.Fatalf("scanHosts(open) = %#v", found)
	}
	// 同一主机的未监听端口不应出现在结果中
	if none := scanHosts(context.Background(), []string{host}, 1, 500*time.Millisecond); len(none) != 0 {
		t.Fatalf("scanHosts(closed) = %#v", none)
	}
}

func TestDiscoverHostsAPI(t *testing.T) {
	app := jumpTestApp(t)
	addr := startTestSSHServer(t)
	host, port, _ := splitHostPort(t, addr)
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := loggedInClient(t, server.URL, "admin", "very-secure-password")

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/discovery", strings.NewReader(`{"cidr":"`+host+`/32","port":`+strconv.Itoa(port)+`,"timeout_ms":1000}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("discovery status = %d", response.StatusCode)
	}
	body, _ := readAll(t, response)
	if !strings.Contains(body, `"is_ssh":true`) {
		t.Fatalf("discovery body = %s", body)
	}
}

func TestDiscoverHostsForbiddenForOperator(t *testing.T) {
	app := jumpTestApp(t)
	if _, err := app.store.CreateUser("operator2", "operator2-password-1", RoleOperator, "prod"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := loggedInClient(t, server.URL, "operator2", "operator2-password-1")
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/discovery", strings.NewReader(`{"cidr":"127.0.0.1/32"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("operator discovery status = %d", response.StatusCode)
	}
}

func readAll(t *testing.T, response *http.Response) (string, error) {
	t.Helper()
	data := make([]byte, 0, 1024)
	buffer := make([]byte, 256)
	for {
		n, err := response.Body.Read(buffer)
		data = append(data, buffer[:n]...)
		if err != nil {
			break
		}
	}
	return string(data), nil
}

func splitHostPort(t *testing.T, addr string) (string, int, string) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port, addr
}
