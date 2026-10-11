package reverse

import (
	"strconv"
	"strings"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/xtaci/smux"
)

const (
	defaultBindHost     = "127.0.0.1"
	defaultStreamBuffer = 262144
	defaultIdleTimeout  = 60
	defaultMaxListeners = 1
)

// portRange is an inclusive [Min, Max] port range
type portRange struct {
	Min int
	Max int
}

func (r portRange) contains(port int) bool {
	return port >= r.Min && port <= r.Max
}

// parsePortRanges parses allow-ports entries in "27061-27099" (range) or "27061" (single) form
func parsePortRanges(entries []string) ([]portRange, error) {
	ranges := make([]portRange, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var minPort, maxPort int
		var err error
		if idx := strings.IndexByte(entry, '-'); idx >= 0 {
			if minPort, err = parsePort(entry[:idx]); err != nil {
				return nil, err
			}
			if maxPort, err = parsePort(entry[idx+1:]); err != nil {
				return nil, err
			}
			if minPort > maxPort {
				return nil, common.NewError("invalid reverse allow-ports range " + entry)
			}
		} else {
			if minPort, err = parsePort(entry); err != nil {
				return nil, err
			}
			maxPort = minPort
		}
		ranges = append(ranges, portRange{Min: minPort, Max: maxPort})
	}
	return ranges, nil
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, common.NewError("invalid reverse port " + s)
	}
	if port < 1 || port > 65535 {
		return 0, common.NewError("reverse port out of range " + s)
	}
	return port, nil
}

type UserConfig struct {
	Password     string   `json:"password" yaml:"password"`
	AllowPorts   []string `json:"allow_ports" yaml:"allow-ports"`
	BindHost     string   `json:"bind_host" yaml:"bind-host"`
	MaxListeners int      `json:"max_listeners" yaml:"max-listeners"`
}

// userACL is the resolved server-side view of a UserConfig, keyed by password hash
type userACL struct {
	bindHost    string
	maxListener int
	allowPorts  []portRange
}

// allows reports whether the port passes the ACL: privileged ports are always
// refused and the port must be inside one of the allow-ports ranges
func (u *userACL) allows(port int) bool {
	if port < 1024 {
		return false
	}
	for _, r := range u.allowPorts {
		if r.contains(port) {
			return true
		}
	}
	return false
}

// ForwardConfig is a client-side (daemon mode) forward entry: bind RemotePort
// on the server and relay every connection to the local Target
type ForwardConfig struct {
	RemotePort int    `json:"remote_port" yaml:"remote-port"`
	Target     string `json:"target" yaml:"target"`
}

// Section 是 reverse 节本体(trojan-go 约定:顶层节内容是注册 Config 的字段,
// 见 shadowsocks/router 同款包装)。
type Section struct {
	Enabled         bool   `json:"enabled" yaml:"enabled"`
	DefaultBindHost string `json:"default_bind_host" yaml:"default-bind-host"`
	IdleTimeout     int    `json:"idle_timeout" yaml:"idle-timeout"`
	StreamBuffer    int    `json:"stream_buffer" yaml:"stream-buffer"`
	// StreamIdleTimeout 流级闲置掐断(idleTimeoutConn),与 IdleTimeout 的
	// session 健康检测职责(smux keepalive 间隔/ping 间隔)解耦:
	// 0=未配置,跟随 IdleTimeout(向后兼容);-1=禁用流掐断(长连语义,
	// 僵尸回收交由 session 死亡全量回收)。
	StreamIdleTimeout int `json:"stream_idle_timeout" yaml:"stream-idle-timeout"`
	// FrameMin/FrameMax smux 帧级随机化:每帧 payload 在 [frame-min, frame-max]
	// 均匀随机(两者须同时>0 启用,0=关闭,固定 MaxFrameSize 切分向后兼容)。
	// 动机:TLS record 头 5B 明文长度 DPI 直读,固定分片→固定 record 周期
	// =可统计指纹;随机帧长→随机 record 长。线上协议零改动(帧头自带长度,
	// 旧对端照常解析,仅发送侧行为)。
	FrameMin int `json:"frame_min" yaml:"frame-min"`
	FrameMax int `json:"frame_max" yaml:"frame-max"`
	// KeepaliveJitter keepalive/ping 间隔的 ±随机比例(0.2=±20%,0=关闭):
	// 打散固定周期 NOP/ping 信标。
	KeepaliveJitter float64          `json:"keepalive_jitter" yaml:"keepalive-jitter"`
	Users           []UserConfig     `json:"users" yaml:"users"`
	Forwards        []ForwardConfig  `json:"forwards" yaml:"forwards"`
}

// streamIdleDuration 流级闲置掐断时长:显式配置优先(-1→0=不包裹),
// 未配置跟随 IdleTimeout。
func (s Section) streamIdleDuration() time.Duration {
	if s.StreamIdleTimeout != 0 {
		return idleTimeoutDuration(s.StreamIdleTimeout)
	}
	return idleTimeoutDuration(s.IdleTimeout)
}

// applyFrameShaping 把帧级随机化与 keepalive 抖动参数注入 smux 配置
// (client/server 两侧 smux 会话组装处共用)。未配置时零改动(向后兼容)。
func applyFrameShaping(smuxConfig *smux.Config, sec Section) {
	if sec.FrameMin > 0 && sec.FrameMax >= sec.FrameMin {
		smuxConfig.FrameSizeMin = sec.FrameMin
		smuxConfig.FrameSizeMax = sec.FrameMax
	}
	if sec.KeepaliveJitter > 0 {
		smuxConfig.KeepAliveJitter = sec.KeepaliveJitter
	}
}

// Config 是注册到 config 体系的顶层结构,YAML `reverse:` 节经 Reverse 字段落位。
// 库式注入(config.WithConfig)同样传 &Config{Reverse: Section{...}}。
type Config struct {
	Reverse Section `json:"reverse" yaml:"reverse"`
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return &Config{
			Reverse: Section{
				Enabled:         false,
				DefaultBindHost: defaultBindHost,
				IdleTimeout:     defaultIdleTimeout,
				StreamBuffer:    defaultStreamBuffer,
			},
		}
	})
}
