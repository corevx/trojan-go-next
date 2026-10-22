package client

import (
	"reflect"
	"testing"
)

// TestGenerateClientTree 锁定出站栈组装逻辑（bool→StackOptions 收敛重构
// 与 NAT router 解禁共用此函数，行为必须与原先位置参数版本一致）。
func TestGenerateClientTree(t *testing.T) {
	cases := []struct {
		name string
		opts StackOptions
		want []string
	}{
		{
			name: "plain trojan over tls",
			opts: StackOptions{},
			want: []string{"TRANSPORT", "TLS", "TROJAN"},
		},
		{
			name: "websocket",
			opts: StackOptions{Websocket: true},
			want: []string{"TRANSPORT", "TLS", "WEBSOCKET", "TROJAN"},
		},
		{
			name: "shadowsocks",
			opts: StackOptions{Shadowsocks: true},
			want: []string{"TRANSPORT", "TLS", "SHADOWSOCKS", "TROJAN"},
		},
		{
			name: "mux",
			opts: StackOptions{Mux: true},
			want: []string{"TRANSPORT", "TLS", "TROJAN", "MUX", "SIMPLESOCKS"},
		},
		{
			name: "router",
			opts: StackOptions{Router: true},
			want: []string{"TRANSPORT", "TLS", "TROJAN", "ROUTER"},
		},
		{
			name: "transport plugin replaces tls",
			opts: StackOptions{TransportPlugin: true},
			want: []string{"TRANSPORT", "TROJAN"},
		},
		{
			name: "reality replaces tls",
			opts: StackOptions{Reality: true},
			want: []string{"TRANSPORT", "REALITY", "TROJAN"},
		},
		{
			name: "reality with mux",
			opts: StackOptions{Reality: true, Mux: true},
			want: []string{"TRANSPORT", "REALITY", "TROJAN", "MUX", "SIMPLESOCKS"},
		},
		{
			name: "reality with shadowsocks and router",
			opts: StackOptions{Reality: true, Shadowsocks: true, Router: true},
			want: []string{"TRANSPORT", "REALITY", "SHADOWSOCKS", "TROJAN", "ROUTER"},
		},
		{
			name: "full stack",
			opts: StackOptions{Websocket: true, Shadowsocks: true, Mux: true, Router: true},
			want: []string{"TRANSPORT", "TLS", "WEBSOCKET", "SHADOWSOCKS", "TROJAN", "MUX", "SIMPLESOCKS", "ROUTER"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GenerateClientTree(c.opts)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("GenerateClientTree(%+v) = %v, want %v", c.opts, got, c.want)
			}
		})
	}
}
