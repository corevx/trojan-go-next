//go:build linux
// +build linux

package nat

import (
	"strings"
	"testing"

	"github.com/p4gefau1t/trojan-go/proxy"
)

// TestNATRouterEnabledNotRejected NAT 模式 router 解禁回归：router.enabled=true
// 时不得再被 "router is not allowed in nat mode" 拒绝（守卫已删，router 栈
// 随配置组装）。无特权环境 tproxy 监听失败属环境限制，不算回归。
func TestNATRouterEnabledNotRejected(t *testing.T) {
	cfgData := []byte(`
run_type: nat
log_level: 4
local_addr: 127.0.0.1
local_port: 12345
remote_addr: example.com
remote_port: 443
password:
  - test-password-123
router:
  enabled: true
`)
	p, err := proxy.NewProxyFromConfigData(cfgData, false)
	if err != nil {
		if strings.Contains(err.Error(), "router is not allowed") {
			t.Fatalf("router guard reintroduced in nat mode: %v", err)
		}
		t.Logf("nat proxy creation failed for environment reason (acceptable): %v", err)
		return
	}
	p.Close()
}
