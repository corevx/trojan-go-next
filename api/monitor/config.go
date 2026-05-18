package monitor

const Name = "MONITOR_SERVICE"

type MonitorConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	MonitorHost string `json:"monitor_addr" yaml:"monitor-addr"`
	MonitorPort int    `json:"monitor_port" yaml:"monitor-port"`
}
