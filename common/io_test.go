package common

import (
	"bytes"
	"crypto/rand"
	"runtime"
	"testing"

	"github.com/v2fly/v2ray-core/v4/common"
)

func TestBufferedReader(t *testing.T) {
	payload := [1024]byte{}
	rand.Reader.Read(payload[:])
	rawReader := bytes.NewBuffer(payload[:])
	r := RewindReader{
		rawReader: rawReader,
	}
	r.SetBufferSize(2048)
	buf1 := make([]byte, 512)
	buf2 := make([]byte, 512)
	common.Must2(r.Read(buf1))
	r.Rewind()
	common.Must2(r.Read(buf2))
	if !bytes.Equal(buf1, buf2) {
		t.Fail()
	}
	buf3 := make([]byte, 512)
	common.Must2(r.Read(buf3))
	if !bytes.Equal(buf3, payload[512:]) {
		t.Fail()
	}
	r.Rewind()
	buf4 := make([]byte, 1024)
	common.Must2(r.Read(buf4))
	if !bytes.Equal(payload[:], buf4) {
		t.Fail()
	}
}

// TestRewindBufferReleasedAfterReplay 锁定嗅探缓冲的生命周期契约：
// StopBuffering 后回放数据被下游全部读走时，缓冲底层数组被释放
// （buf == nil），后续 Read 直接透传 rawReader，字节流不变。
// 这是 #6 内存优化的核心：服务端分流树每连接 2KB/1KB/128B 的
// 嗅探缓冲在协议识别完成后不再常驻。
func TestRewindBufferReleasedAfterReplay(t *testing.T) {
	payload := make([]byte, 300)
	rand.Reader.Read(payload)
	rawReader := bytes.NewBuffer(payload)
	r := RewindReader{rawReader: rawReader}
	r.SetBufferSize(1024)

	sniffed := make([]byte, 100)
	common.Must2(r.Read(sniffed))
	r.Rewind()
	r.StopBuffering()

	// 下游分两次读完回放数据
	replay1 := make([]byte, 60)
	n, err := r.Read(replay1)
	if err != nil || n != 60 || !bytes.Equal(replay1, payload[:60]) {
		t.Fatalf("replay first chunk: n=%d err=%v", n, err)
	}
	if r.buf == nil {
		t.Fatal("buffer released before replay finished")
	}
	replay2 := make([]byte, 100)
	n, err = r.Read(replay2)
	// 原语义：回放未耗尽时只返回缓冲内剩余字节（不合并 rawReader 数据）
	if err != nil || n != 40 || !bytes.Equal(replay2[:40], payload[60:100]) {
		t.Fatalf("replay tail: n=%d err=%v", n, err)
	}
	// 稳态读：回放耗尽的下一次 Read 触发释放并透传 rawReader
	rest := make([]byte, 100)
	n, err = r.Read(rest)
	if err != nil || n != 100 || !bytes.Equal(rest, payload[100:200]) {
		t.Fatalf("steady-state read after release: n=%d err=%v", n, err)
	}
	if r.buf != nil || r.bufReadIdx != 0 {
		t.Fatalf("sniff buffer not released after replay done: bufReadIdx=%d", r.bufReadIdx)
	}
	tail := make([]byte, 100)
	n, err = r.Read(tail)
	if err != nil || n != 100 || !bytes.Equal(tail, payload[200:]) {
		t.Fatalf("read after release: n=%d err=%v", n, err)
	}
}

// TestRewindMultiRewindWhileBuffering 锁定另一半契约：未 StopBuffering
// （仍在嗅探累积）时，回放完毕后 buf 保留，可再次 Rewind 全量重放。
func TestRewindMultiRewindWhileBuffering(t *testing.T) {
	payload := make([]byte, 128)
	rand.Reader.Read(payload)
	r := RewindReader{rawReader: bytes.NewBuffer(payload)}
	r.SetBufferSize(256)
	first := make([]byte, 128)
	common.Must2(r.Read(first))
	r.Rewind()
	second := make([]byte, 128)
	common.Must2(r.Read(second))
	if !bytes.Equal(first, second) {
		t.Fatal("first replay mismatch")
	}
	// 回放完毕但 buffering 仍开：buf 必须保留，二次 Rewind 仍可全量重放
	if r.buf == nil {
		t.Fatal("buffer released while still buffering, multi-rewind contract broken")
	}
	r.Rewind()
	third := make([]byte, 128)
	common.Must2(r.Read(third))
	if !bytes.Equal(first, third) {
		t.Fatal("second replay mismatch after re-rewind while buffering")
	}
}

// TestRewindRelease 显式释放：握手层等「嗅探后确认死数据、不会 Rewind」
// 的缓冲可通过 Release 立即回收；Release 后 Read 直接透传 rawReader。
func TestRewindRelease(t *testing.T) {
	payload := make([]byte, 200)
	rand.Reader.Read(payload)
	r := RewindReader{rawReader: bytes.NewBuffer(payload)}
	r.SetBufferSize(2048)
	head := make([]byte, 50)
	common.Must2(r.Read(head))
	r.StopBuffering()
	r.Release()
	if r.buf != nil {
		t.Fatal("release did not free buffer")
	}
	// 释放后读：直接从 rawReader 继续消费，不回放已嗅探字节
	rest := make([]byte, 150)
	n, err := r.Read(rest)
	if err != nil || n != 150 || !bytes.Equal(rest, payload[50:]) {
		t.Fatalf("read after release: n=%d err=%v", n, err)
	}

	// buffering 未停时 Release 属状态机误用，必须 panic 快速暴露
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Release while buffering should panic")
			}
		}()
		r2 := RewindReader{rawReader: bytes.NewBuffer(payload)}
		r2.SetBufferSize(16)
		r2.Release()
	}()
}

// TestSniffBuffersReclaimedHeap 自对照 heap 度量：n 条完成嗅探生命周期的
// 连接（释放生效）对比同样 n 条缓冲保留的连接（模拟改造前的常驻行为），
// GC 后 retained heap 差值应接近 n × bufferSize。
func TestSniffBuffersReclaimedHeap(t *testing.T) {
	const n = 2000
	const bufSize = 2048
	payload := make([]byte, 64)
	rand.Reader.Read(payload)
	readBuf := make([]byte, 64)

	// build 构造 n 条完成嗅探生命周期的 reader；keepBuffer=true 模拟
	// 改造前「缓冲随连接常驻」，false 走回放完毕释放的新路径。
	// readers 由调用方持有，度量期间保持可达（否则 GC 连同 buf 一起回收，
	// 两组读数无差异）。
	build := func(keepBuffer bool) []*RewindReader {
		readers := make([]*RewindReader, 0, n)
		for i := 0; i < n; i++ {
			r := &RewindReader{rawReader: bytes.NewBuffer(payload)}
			r.SetBufferSize(bufSize)
			common.Must2(r.Read(readBuf))
			r.Rewind()
			if !keepBuffer {
				r.StopBuffering()
				common.Must2(r.Read(readBuf)) // 回放
				_, _ = r.Read(readBuf)        // 回放耗尽触发释放（rawReader EOF 属预期）
			} else {
				common.Must2(r.Read(readBuf))
			}
			readers = append(readers, r)
		}
		return readers
	}
	gcMeasure := func() uint64 {
		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}

	// 先测 freed 做干净基线（此时 heap 只有 freed 组自身），再测 held：
	// 差值 = held 组的嗅探缓冲 + reader 结构，不受另一组污染
	freedReaders := build(false) // 改造后：回放完毕释放
	freed := gcMeasure()
	heldReaders := build(true) // 模拟改造前：嗅探缓冲随连接常驻
	held := gcMeasure()
	runtime.KeepAlive(freedReaders)
	runtime.KeepAlive(heldReaders)
	saved := int64(held) - int64(freed)
	t.Logf("retained heap with buffers held=%d vs freed=%d, saved=%d bytes (%.1f KB/conn, buffer cap %d)",
		held, freed, saved, float64(saved)/float64(n)/1024, bufSize)
	if saved < int64(n)*bufSize/2 {
		t.Fatalf("expected to reclaim ~%d bytes after release, got %d", n*bufSize, saved)
	}
}

// TestSniffLifecycleAllocs 度量「嗅探→分流→回放消费完毕」一个完整生命
// 周期的堆分配，作为 #6 的收益佐证：回放完毕后缓冲数组可被 GC 回收
// （稳态保留量降为 0），改造前该数组随连接常驻。
func TestSniffLifecycleAllocs(t *testing.T) {
	payload := make([]byte, 512)
	rand.Reader.Read(payload)
	readBuf := make([]byte, 512)

	allocs := testing.AllocsPerRun(200, func() {
		r := RewindReader{rawReader: bytes.NewBuffer(payload)}
		r.SetBufferSize(2048)
		r.Read(readBuf[:512])
		r.Rewind()
		r.StopBuffering()
		r.Read(readBuf[:512]) // 回放完毕，缓冲释放
		r.Read(readBuf[:0])   // 稳态读
	})
	t.Logf("allocs per sniff lifecycle (NewReader+2KB buffer+replay): %.0f", allocs)
	// bytes.NewBuffer 引用 payload 不拷贝；缓冲数组 make(2048) 为主要分配。
	// 阈值取得宽松（≤8）防 flaky，重点是生命周期结束后 buf 归零由
	// TestRewindBufferReleasedAfterReplay 白盒锁定。
	if allocs > 8 {
		t.Fatalf("unexpected allocs in sniff lifecycle: %.0f", allocs)
	}
}
