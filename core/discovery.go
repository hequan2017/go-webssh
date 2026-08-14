package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	discoveryMaxAddresses = 1024
	discoveryWorkers      = 64
	discoveryBanner       = "SSH-2.0-GoWebssh-Discovery\r\n"
)

type discoveryRequest struct {
	CIDR      string `json:"cidr"`
	Port      int    `json:"port"`
	TimeoutMS int    `json:"timeout_ms"`
}

type DiscoveredHost struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	SSHBanner string `json:"ssh_banner,omitempty"`
	IsSSH     bool   `json:"is_ssh"`
}

// discoverHosts 仅管理员可用：对指定网段并发探测 SSH 端口并读取版本横幅，不做任何登录尝试。
func (a *Application) discoverHosts(w http.ResponseWriter, r *http.Request) {
	var input discoveryRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "发现参数无效")
		return
	}
	if input.Port < 1 || input.Port > 65535 {
		input.Port = 22
	}
	if input.TimeoutMS < 200 || input.TimeoutMS > 5000 {
		input.TimeoutMS = 1500
	}
	hosts, err := expandCIDR(input.CIDR)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	results := scanHosts(r.Context(), hosts, input.Port, time.Duration(input.TimeoutMS)*time.Millisecond)
	_ = a.store.AppendAudit(a.auditFor(r, "asset.discover", "network", input.CIDR, true, map[string]any{"port": input.Port, "hosts": len(hosts), "found": len(results)}))
	writeJSON(w, http.StatusOK, map[string]any{"hosts": results})
}

// expandCIDR 展开网段为地址列表，支持 CIDR 和单个 IP；超过 1024 个地址时拒绝。
func expandCIDR(cidr string) ([]string, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return nil, fmt.Errorf("网段不能为空")
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		addr, addrErr := netip.ParseAddr(cidr)
		if addrErr != nil {
			return nil, fmt.Errorf("网段格式无效，示例 192.168.1.0/24 或 192.168.1.10")
		}
		prefix = netip.PrefixFrom(addr, addr.BitLen())
	}
	prefix = prefix.Masked()
	hostBits := prefix.Addr().BitLen() - prefix.Bits()
	if hostBits > 10 {
		return nil, fmt.Errorf("网段地址数量超过 %d，请缩小范围", discoveryMaxAddresses)
	}
	count := 1 << hostBits
	limit := count
	if count > 2 {
		limit = count - 2 // 跳过网络地址和广播地址
	}
	addresses := make([]string, 0, limit)
	addr := prefix.Addr()
	if count > 2 {
		addr = addr.Next()
	}
	for i := 0; i < limit; i++ {
		addresses = append(addresses, addr.String())
		addr = addr.Next()
	}
	return addresses, nil
}

func scanHosts(ctx context.Context, hosts []string, port int, timeout time.Duration) []DiscoveredHost {
	workers := discoveryWorkers
	if len(hosts) < workers {
		workers = len(hosts)
	}
	if workers == 0 {
		return []DiscoveredHost{}
	}
	jobs := make(chan string)
	found := make([]DiscoveredHost, 0, len(hosts)/4)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range jobs {
				if hostResult, ok := probeSSH(ctx, host, port, timeout); ok {
					mu.Lock()
					found = append(found, hostResult)
					mu.Unlock()
				}
			}
		}()
	}
	for _, host := range hosts {
		jobs <- host
	}
	close(jobs)
	wg.Wait()
	sort.Slice(found, func(i, j int) bool {
		a, errA := netip.ParseAddr(found[i].Host)
		b, errB := netip.ParseAddr(found[j].Host)
		if errA == nil && errB == nil {
			return a.Compare(b) < 0
		}
		return found[i].Host < found[j].Host
	})
	return found
}

// probeSSH 先发送客户端版本行再读取对端横幅：SSH 服务必定回送自身版本，据此区分 SSH 与其他服务。
func probeSSH(ctx context.Context, host string, port int, timeout time.Duration) (DiscoveredHost, bool) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return DiscoveredHost{}, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	result := DiscoveredHost{Host: host, Port: port}
	if _, err := conn.Write([]byte(discoveryBanner)); err != nil {
		return result, true
	}
	buffer := make([]byte, 256)
	n, err := conn.Read(buffer)
	if err != nil || n == 0 {
		return result, true // 端口开放但未回送横幅
	}
	banner := strings.TrimSpace(string(buffer[:n]))
	result.SSHBanner = banner
	result.IsSSH = strings.HasPrefix(banner, "SSH-")
	return result, true
}
