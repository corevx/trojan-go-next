package reverse

import (
	"testing"
	"time"
)

func TestStreamIdleDuration(t *testing.T) {
	cases := []struct {
		name string
		s    Section
		want time.Duration
	}{
		{"未配置跟随 IdleTimeout", Section{IdleTimeout: 60}, 60 * time.Second},
		{"显式禁用", Section{IdleTimeout: 60, StreamIdleTimeout: -1}, 0},
		{"显式覆盖", Section{IdleTimeout: 60, StreamIdleTimeout: 300}, 300 * time.Second},
	}
	for _, c := range cases {
		if got := c.s.streamIdleDuration(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
