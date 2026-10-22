package shadowtls

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
)

// 三条 HMAC 链（规格 §4.3），均为 HMAC-SHA1(password) 流式链、4 字节截断：
//   - 链C（客户端→服务端数据帧）：种子 serverRandom ‖ "C"，tag 回喂
//   - 链S（服务端→客户端数据帧）：种子 serverRandom ‖ "S"，tag 回喂
//   - 链R（握手期真站→客户端改写帧）：种子 serverRandom（无侧标签），tag 不回喂
// XOR 密钥 kdf = SHA256(password ‖ serverRandom)，仅作用于握手期改写帧。

// hmacReset 将链重置回种子态。side 为 0 表示链R（无侧标签）。
func hmacReset(h hash.Hash, serverRandom []byte, side byte) {
	h.Reset()
	_, _ = h.Write(serverRandom)
	if side != 0 {
		_, _ = h.Write([]byte{side})
	}
}

func kdf(password string, serverRandom []byte) []byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(password))
	_, _ = hasher.Write(serverRandom)
	return hasher.Sum(nil)
}

func xorSlice(data, key []byte) {
	for i := range data {
		data[i] ^= key[i%len(key)]
	}
}

// sendAlert 发送 31 字节随机填充的 alert 帧，作为本地验证失败时的回应，
// 与真实 TLS 栈的 alert 行为无法区分（规格 §4.10）
func sendAlert(writer io.Writer) {
	record := [verificationAlertSize]byte{recordAlert, 3, 3, 0, verificationAlertSize - tlsHeaderSize}
	if _, err := rand.Read(record[tlsHeaderSize:]); err == nil {
		_, _ = writer.Write(record[:])
	}
}

// verifyClientHello 逐用户验签 ClientHello 记录（规格 §4.1）：
// 以 4 字节全零代入 session_id 末 4 字节重算 HMAC-SHA1 截断 4 字节，
// 与 frame[72:76] 比对；命中即返回该用户。
func verifyClientHello(frame []byte, users []user) (*user, error) {
	const minLength = tlsHeaderSize + handshakeHeaderSize + 2 + tlsRandomSize + 1 + tlsSessionIDSize
	const hmacIndex = sessionIDLengthIndex + 1 + tlsSessionIDSize - hmacSize
	if len(frame) < minLength {
		return nil, common.NewError("truncated ClientHello record")
	}
	if frame[0] != recordHandshake || frame[tlsHeaderSize] != handshakeClientHello {
		return nil, common.NewError("unexpected ClientHello record type")
	}
	if frame[sessionIDLengthIndex] != tlsSessionIDSize {
		return nil, common.NewError("unexpected session id length")
	}
	for i := range users {
		u := &users[i]
		h := hmac.New(sha1.New, []byte(u.Password))
		_, _ = h.Write(frame[tlsHeaderSize:hmacIndex])
		_, _ = h.Write([]byte{0, 0, 0, 0})
		_, _ = h.Write(frame[hmacIndex+hmacSize:])
		if hmac.Equal(frame[hmacIndex:hmacIndex+hmacSize], h.Sum(nil)[:hmacSize]) {
			return u, nil
		}
	}
	return nil, common.NewError("ClientHello HMAC mismatch")
}

// copyClientUntilAuth 握手期客户端→真站方向的中继：非 appdata 帧原样转发；
// 对每个 appdata 帧以链C 种子态试签（不命中链不前进），命中即视为 auth 帧
// （客户端握手完成后的首个数据写操作，规格 §4.2），以 payload+tag 推进链C
// 后返回 payload。重播种与克隆两法等价，此处采用重播种实现。
func copyClientUntilAuth(clientConn, destConn net.Conn, hmacVerify hash.Hash, resetVerify func()) ([]byte, error) {
	for {
		frame, err := readFrame(clientConn)
		if err != nil {
			return nil, common.NewError("failed to read client record").Base(err)
		}
		if len(frame) > tlsHMACHeaderSize && frame[0] == recordApplicationData {
			resetVerify()
			_, _ = hmacVerify.Write(frame[tlsHMACHeaderSize:])
			tag := hmacVerify.Sum(nil)[:hmacSize]
			if hmac.Equal(tag, frame[tlsHeaderSize:tlsHMACHeaderSize]) {
				resetVerify()
				_, _ = hmacVerify.Write(frame[tlsHMACHeaderSize:])
				_, _ = hmacVerify.Write(tag)
				return append([]byte(nil), frame[tlsHMACHeaderSize:]...), nil
			}
		}
		if _, err = destConn.Write(frame); err != nil {
			return nil, common.NewError("failed to write client record to dest").Base(err)
		}
	}
}

// copyDestWithRewrite 握手期真站→客户端方向的中继（规格 §4.5）：
// handshake/CCS/alert 帧原样转发；appdata 帧（TLS1.3 加密握手段）payload
// 先 XOR kdf 再喂链R（喂的是 XOR 后的数据），线上帧改写为
// [type|ver|len+4|tag4|payload']。
func copyDestWithRewrite(destConn, clientConn net.Conn, password string, serverRandom []byte, hmacWrite hash.Hash) error {
	writeKey := kdf(password, serverRandom)
	for {
		frame, err := readFrame(destConn)
		if err != nil {
			return common.NewError("failed to read dest record").Base(err)
		}
		if frame[0] != recordApplicationData {
			if _, err = clientConn.Write(frame); err != nil {
				return common.NewError("failed to write dest record to client").Base(err)
			}
			continue
		}
		xorSlice(frame[tlsHeaderSize:], writeKey)
		_, _ = hmacWrite.Write(frame[tlsHeaderSize:])
		binary.BigEndian.PutUint16(frame[3:5], uint16(len(frame)-tlsHeaderSize+hmacSize))
		tag := hmacWrite.Sum(nil)[:hmacSize]
		if err = writeBuffers(clientConn, frame[:tlsHeaderSize], tag, frame[tlsHeaderSize:]); err != nil {
			return common.NewError("failed to write modified dest record").Base(err)
		}
	}
}

// relayHandshakeV3 运行握手期双向中继，auth 帧命中即结束：
// 客户端方向命中 ⇒ 关真站连接、等待真站方向退出、返回 auth 帧 payload。
// 任一方向出错 ⇒ 双向关闭，返回首个错误。
func relayHandshakeV3(clientConn, destConn net.Conn, password string, serverRandom []byte, hmacWrite, hmacVerify hash.Hash, resetVerify func()) ([]byte, error) {
	type result struct {
		request []byte
		client  bool
		err     error
	}
	results := make(chan result, 2)
	go func() {
		request, err := copyClientUntilAuth(clientConn, destConn, hmacVerify, resetVerify)
		results <- result{request: request, client: true, err: err}
	}()
	go func() {
		err := copyDestWithRewrite(destConn, clientConn, password, serverRandom, hmacWrite)
		results <- result{err: err}
	}()
	first := <-results
	if first.client && first.err == nil {
		_ = destConn.Close()
		<-results
		return first.request, nil
	}
	_ = destConn.Close()
	_ = clientConn.SetDeadline(time.Now())
	second := <-results
	if first.err != nil {
		return nil, first.err
	}
	return nil, second.err
}

// verifiedConn 是鉴权后的连接：双向数据均包装为伪 TLS application_data
// 记录 + 逐帧 HMAC（链S 写 / 链C 读，tag 回喂），规格 §4.4。
type verifiedConn struct {
	net.Conn
	writeMu    sync.Mutex
	hmacAdd    hash.Hash // 链S：写侧生成
	hmacVerify hash.Hash // 链C：读侧验证
	pending    []byte
	readBuffer []byte
	readOffset int
}

func newVerifiedConn(conn net.Conn, hmacAdd, hmacVerify hash.Hash) *verifiedConn {
	return &verifiedConn{
		Conn:       conn,
		hmacAdd:    hmacAdd,
		hmacVerify: hmacVerify,
	}
}

func (c *verifiedConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		return c.readPending(p), nil
	}
	for {
		frame, err := c.readRecord()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// 超时只中断当前 Read：保留半帧状态，下次 Read 继续
				return 0, err
			}
			sendAlert(c.Conn)
			return 0, err
		}
		switch frame[0] {
		case recordAlert:
			return 0, common.NewError("remote alert").Base(net.ErrClosed)
		case recordApplicationData:
			if !verifyApplicationData(frame, c.hmacVerify, true) {
				sendAlert(c.Conn)
				return 0, common.NewError("application data verification failed")
			}
			c.pending = frame[tlsHMACHeaderSize:]
			return c.readPending(p), nil
		default:
			sendAlert(c.Conn)
			return 0, common.NewError("unexpected TLS record type " + strconv.Itoa(int(frame[0])))
		}
	}
}

// readRecord 完整读入一个记录；半帧状态跨调用保留，使 deadline 只中断
// 当前 Read 而不丢已读字节
func (c *verifiedConn) readRecord() ([]byte, error) {
	if c.readBuffer == nil {
		c.readBuffer = make([]byte, tlsHeaderSize)
	}
	if c.readOffset < tlsHeaderSize {
		n, err := io.ReadFull(c.Conn, c.readBuffer[c.readOffset:tlsHeaderSize])
		c.readOffset += n
		if err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(c.readBuffer[3:]))
		if length < hmacSize {
			return nil, common.NewError("malformed record length")
		}
		c.readBuffer = append(c.readBuffer, make([]byte, length)...)
	}
	if c.readOffset < len(c.readBuffer) {
		n, err := io.ReadFull(c.Conn, c.readBuffer[c.readOffset:])
		c.readOffset += n
		if err != nil {
			return nil, err
		}
	}
	frame := c.readBuffer
	c.readBuffer = nil
	c.readOffset = 0
	return frame, nil
}

func (c *verifiedConn) readPending(p []byte) int {
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n
}

func (c *verifiedConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	total := len(p)
	for len(p) > 0 {
		length := len(p)
		if length > maxTLSPlaintext {
			length = maxTLSPlaintext
		}
		if err := c.writeRecord(p[:length]); err != nil {
			return total - len(p), err
		}
		p = p[length:]
	}
	return total, nil
}

func (c *verifiedConn) writeRecord(p []byte) error {
	var header [tlsHMACHeaderSize]byte
	header[0] = recordApplicationData
	header[1] = 3
	header[2] = 3
	binary.BigEndian.PutUint16(header[3:tlsHeaderSize], uint16(hmacSize+len(p)))
	_, _ = c.hmacAdd.Write(p)
	tag := c.hmacAdd.Sum(nil)[:hmacSize]
	_, _ = c.hmacAdd.Write(tag)
	copy(header[tlsHeaderSize:], tag)
	return writeBuffers(c.Conn, header[:], p)
}

// verifyApplicationData 验证 appdata 帧的链 tag；update 为 true 时 tag 回喂
func verifyApplicationData(frame []byte, h hash.Hash, update bool) bool {
	if len(frame) < tlsHMACHeaderSize || frame[0] != recordApplicationData || frame[1] != 3 || frame[2] != 3 {
		return false
	}
	_, _ = h.Write(frame[tlsHMACHeaderSize:])
	tag := h.Sum(nil)[:hmacSize]
	if update {
		_, _ = h.Write(tag)
	}
	return hmac.Equal(frame[tlsHeaderSize:tlsHMACHeaderSize], tag)
}

// prefixConn 把 auth 帧捕获的 payload 重新垫回读流头部，内层 trojan 协议
// 从首个数据字节开始消费
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}
