package common

import (
	"io"
	"net"
	"sync"

	"github.com/p4gefau1t/trojan-go/log"
)

type RewindReader struct {
	mu         sync.Mutex
	rawReader  io.Reader
	buf        []byte
	bufReadIdx int
	rewound    bool
	buffering  bool
	bufferSize int
}

func (r *RewindReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rewound {
		if len(r.buf) > r.bufReadIdx {
			n := copy(p, r.buf[r.bufReadIdx:])
			r.bufReadIdx += n
			return n, nil
		}
		r.rewound = false // all buffering content has been read
		// 嗅探阶段结束（StopBuffering 已停追加）且回放数据已全部交付：
		// 缓冲副本再无用途，释放底层数组，避免嗅探缓冲随连接常驻。
		// buffering 仍在开启（未 StopBuffering）时保留 buf，维持
		// 「回放完毕后可再次 Rewind」的原有契约（见 TestBufferedReader）。
		if !r.buffering {
			r.buf = nil
			r.bufReadIdx = 0
		}
	}
	n, err := r.rawReader.Read(p)
	if r.buffering {
		r.buf = append(r.buf, p[:n]...)
		if len(r.buf) > r.bufferSize*2 {
			log.Debug("read too many bytes!")
		}
	}
	return n, err
}

func (r *RewindReader) ReadByte() (byte, error) {
	buf := [1]byte{}
	_, err := r.Read(buf[:])
	return buf[0], err
}

func (r *RewindReader) Discard(n int) (int, error) {
	buf := [128]byte{}
	if n < 128 {
		return r.Read(buf[:n])
	}
	for discarded := 0; discarded+128 < n; discarded += 128 {
		_, err := r.Read(buf[:])
		if err != nil {
			return discarded, err
		}
	}
	if rest := n % 128; rest != 0 {
		return r.Read(buf[:rest])
	}
	return n, nil
}

func (r *RewindReader) Rewind() {
	r.mu.Lock()
	if r.bufferSize == 0 {
		panic("no buffer")
	}
	r.rewound = true
	r.bufReadIdx = 0
	r.mu.Unlock()
}

func (r *RewindReader) StopBuffering() {
	r.mu.Lock()
	r.buffering = false
	r.mu.Unlock()
}

// Release 释放嗅探缓冲的底层数组。仅在已 StopBuffering 且缓冲内容确认
// 不会再被 Rewind 回放时调用（嗅探决策已完成、缓冲内容是死数据的场景，
// 如 TLS 握手成功后握手层缓冲中残留的 ClientHello 副本）。
// 与回放完毕的惰性释放不同，本方法不要求经历过 Rewind；调用后 Read
// 直接透传 rawReader，行为不变。
func (r *RewindReader) Release() {
	r.mu.Lock()
	if r.buffering {
		r.mu.Unlock()
		panic("release while buffering")
	}
	r.buf = nil
	r.bufReadIdx = 0
	r.mu.Unlock()
}

func (r *RewindReader) SetBufferSize(size int) {
	r.mu.Lock()
	if size == 0 { // disable buffering
		if !r.buffering {
			panic("reader is disabled")
		}
		r.buffering = false
		r.buf = nil
		r.bufReadIdx = 0
		r.bufferSize = 0
	} else {
		if r.buffering {
			panic("reader is buffering")
		}
		r.buffering = true
		r.bufReadIdx = 0
		r.bufferSize = size
		r.buf = make([]byte, 0, size)
	}
	r.mu.Unlock()
}

type RewindConn struct {
	net.Conn
	*RewindReader
}

func (c *RewindConn) Read(p []byte) (int, error) {
	return c.RewindReader.Read(p)
}

func NewRewindConn(conn net.Conn) *RewindConn {
	return &RewindConn{
		Conn: conn,
		RewindReader: &RewindReader{
			rawReader: conn,
		},
	}
}

type StickyWriter struct {
	rawWriter   io.Writer
	writeBuffer []byte
	MaxBuffered int
}

func (w *StickyWriter) Write(p []byte) (int, error) {
	if w.MaxBuffered > 0 {
		w.MaxBuffered--
		w.writeBuffer = append(w.writeBuffer, p...)
		if w.MaxBuffered != 0 {
			return len(p), nil
		}
		w.MaxBuffered = 0
		_, err := w.rawWriter.Write(w.writeBuffer)
		w.writeBuffer = nil
		return len(p), err
	}
	return w.rawWriter.Write(p)
}
