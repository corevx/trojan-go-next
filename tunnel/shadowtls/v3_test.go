package shadowtls

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// 规格依据：demos/07 §4.1——session_id(32B) = rand(28) ‖ HMAC-SHA1(password,
// msg[0:39] ‖ sid28 ‖ 0x00000000 ‖ msg[71:])[:4]，其中 msg 为去掉记录头的
// ClientHello 握手消息。此处为测试侧独立实现（不复用生产代码），用于交叉
// 验证 verifyClientHello。
func testSignClientHello(t *testing.T, frame []byte, password string) (sid [tlsSessionIDSize]byte) {
	t.Helper()
	// 消息坐标：记录头 5 字节去掉后，session_id 起点 = 1+3+2+32+1 = 39
	const msgSessionIDStart = 1 + 3 + 2 + tlsRandomSize + 1
	msg := frame[tlsHeaderSize:]
	if len(msg) < msgSessionIDStart+tlsSessionIDSize {
		t.Fatal("client hello too short")
	}
	_, err := rand.Read(sid[:tlsSessionIDSize-hmacSize])
	if err != nil {
		t.Fatal(err)
	}
	h := hmac.New(sha1.New, []byte(password))
	h.Write(msg[:msgSessionIDStart])
	// HMAC 输入中 session_id 槽位 = 28 字节随机 + 4 字节全零（sid 数组本身
	// 即零值尾部，无需再补写零），与 mihomo generateSessionID 同构
	h.Write(sid[:])
	h.Write(msg[msgSessionIDStart+tlsSessionIDSize:])
	copy(sid[tlsSessionIDSize-hmacSize:], h.Sum(nil)[:hmacSize])
	// 写回记录（记录坐标 frame[44:76]）
	copy(frame[tlsHeaderSize+msgSessionIDStart:], sid[:])
	return sid
}

// craftClientHello 构造一个最小但字段齐全的 ClientHello 记录：
// legacy_version + random(32) + session_id(32) + cipher_suites + compression
// + server_name 扩展。session_id 初始为可辨识模式，供签名函数覆写。
func craftClientHello(sni string) []byte {
	sniBytes := []byte(sni)
	sniExt := make([]byte, 0, 6+len(sniBytes))
	sniExt = binary.BigEndian.AppendUint16(sniExt, 0) // extension type server_name
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(2+1+2+len(sniBytes)))
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(1+2+len(sniBytes))) // server_name list length
	sniExt = append(sniExt, 0)                                                // host_name type
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(len(sniBytes)))
	sniExt = append(sniExt, sniBytes...)

	body := make([]byte, 0, 128)
	body = binary.BigEndian.AppendUint16(body, 0x0303)  // legacy_version TLS1.2
	body = append(body, make([]byte, tlsRandomSize)...) // random（内容不参与签名逻辑）
	body = append(body, tlsSessionIDSize)               // session_id length
	for i := 0; i < tlsSessionIDSize; i++ {             // session_id 占位
		body = append(body, byte(0xAA))
	}
	body = binary.BigEndian.AppendUint16(body, 4) // cipher_suites length
	body = binary.BigEndian.AppendUint16(body, 0x1301)
	body = binary.BigEndian.AppendUint16(body, 0x1302)
	body = append(body, 1, 0) // compression methods: null
	body = binary.BigEndian.AppendUint16(body, uint16(len(sniExt)))
	body = append(body, sniExt...)

	frame := make([]byte, 0, tlsHeaderSize+4+len(body))
	frame = append(frame, recordHandshake, 3, 1)
	frame = binary.BigEndian.AppendUint16(frame, uint16(4+len(body)))
	frame = append(frame, handshakeClientHello)
	frame = append(frame, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	frame = append(frame, body...)
	return frame
}

func TestVerifyClientHelloSignAndVerify(t *testing.T) {
	users := []user{{Name: "u1", Password: "pass1"}, {Name: "u2", Password: "pass2"}}

	frame := craftClientHello("www.example.com")
	sid := testSignClientHello(t, frame, "pass2")

	// 签名记录坐标断言：sid 应精确落在 frame[44:76]
	if !bytes.Equal(frame[sessionIDLengthIndex+1:sessionIDLengthIndex+1+tlsSessionIDSize], sid[:]) {
		t.Fatal("session id not embedded at expected frame offset")
	}
	u, err := verifyClientHello(frame, users)
	if err != nil {
		t.Fatal("verify failed:", err)
	}
	if u.Name != "u2" {
		t.Fatal("expected user u2, got", u.Name)
	}

	// 错误密码签名的 hello 不命中任何用户
	frame2 := craftClientHello("www.example.com")
	testSignClientHello(t, frame2, "wrong-pass")
	if _, err := verifyClientHello(frame2, users); err == nil {
		t.Fatal("expected verify failure with wrong password")
	}

	// 篡改 session_id 一个字节 → 验签失败
	frame3 := craftClientHello("www.example.com")
	testSignClientHello(t, frame3, "pass1")
	frame3[sessionIDLengthIndex+1] ^= 1
	if _, err := verifyClientHello(frame3, users); err == nil {
		t.Fatal("expected verify failure with tampered session id")
	}

	// session_id 长度非 32 → 直接拒绝
	frame4 := craftClientHello("www.example.com")
	frame4[sessionIDLengthIndex] = 31
	if _, err := verifyClientHello(frame4, users); err == nil {
		t.Fatal("expected verify failure with non-32 session id length")
	}

	// 非 ClientHello 记录 → 拒绝
	bad := craftClientHello("www.example.com")
	bad[tlsHeaderSize] = handshakeServerHello
	if _, err := verifyClientHello(bad, users); err == nil {
		t.Fatal("expected verify failure for non ClientHello record")
	}
}

// 规格依据：demos/07 §4.3——链C/S 种子带侧标签且 tag 回喂，链R 种子无
// 侧标签且 tag 不回喂。测试侧独立构造链，与生产链推进互证。
func TestHMACChainSemantics(t *testing.T) {
	password := "secret"
	serverRandom := make([]byte, tlsRandomSize)
	rand.Read(serverRandom)

	// 链C/S：生产实现 = hmacReset + Write(p) + Sum + Write(tag)，与独立构造互证
	produce := func(side byte, payloads ...[]byte) [][]byte {
		h := hmac.New(sha1.New, []byte(password))
		hmacReset(h, serverRandom, side)
		var tags [][]byte
		for _, p := range payloads {
			h.Write(p)
			tag := append([]byte(nil), h.Sum(nil)[:hmacSize]...)
			h.Write(tag)
			tags = append(tags, tag)
		}
		return tags
	}
	independent := func(side byte, payloads ...[]byte) [][]byte {
		h := hmac.New(sha1.New, []byte(password))
		h.Write(serverRandom)
		h.Write([]byte{side})
		var tags [][]byte
		for _, p := range payloads {
			h.Write(p)
			tag := append([]byte(nil), h.Sum(nil)[:hmacSize]...)
			h.Write(tag)
			tags = append(tags, tag)
		}
		return tags
	}
	p1, p2, p3 := []byte("hello"), bytes.Repeat([]byte{0xAB}, 100), []byte("x")
	got := produce('C', p1, p2, p3)
	want := independent('C', p1, p2, p3)
	for i := range got {
		if !hmac.Equal(got[i], want[i]) {
			t.Fatalf("chain C tag %d mismatch", i)
		}
	}
	got = produce('S', p1, p2)
	want = independent('S', p1, p2)
	for i := range got {
		if !hmac.Equal(got[i], want[i]) {
			t.Fatalf("chain S tag %d mismatch", i)
		}
	}

	// 链R：tag 不回喂——两次 Write 的 tag 与顺序累计一致
	hR := hmac.New(sha1.New, []byte(password))
	hmacReset(hR, serverRandom, 0)
	hR.Write(p1)
	tag1 := append([]byte(nil), hR.Sum(nil)[:hmacSize]...)
	hR.Write(p2)
	tag2 := append([]byte(nil), hR.Sum(nil)[:hmacSize]...)

	hInd := hmac.New(sha1.New, []byte(password))
	hInd.Write(serverRandom)
	hInd.Write(p1)
	if !hmac.Equal(tag1, hInd.Sum(nil)[:hmacSize]) {
		t.Fatal("chain R tag1 mismatch")
	}
	hInd.Write(p2)
	if !hmac.Equal(tag2, hInd.Sum(nil)[:hmacSize]) {
		t.Fatal("chain R tag2 mismatch")
	}
}

// 规格依据：demos/07 §4.5——kdf = SHA256(password ‖ serverRandom)，32 字节
// 循环 XOR
func TestKDFAndXOR(t *testing.T) {
	password := "secret"
	serverRandom := []byte("0123456789abcdef0123456789abcdef")
	key := kdf(password, serverRandom)

	h := sha256.New()
	h.Write([]byte(password))
	h.Write(serverRandom)
	if !bytes.Equal(key, h.Sum(nil)) {
		t.Fatal("kdf mismatch with independent SHA256 construction")
	}
	if len(key) != 32 {
		t.Fatal("kdf must be 32 bytes")
	}

	payload := make([]byte, 100)
	rand.Read(payload)
	orig := append([]byte(nil), payload...)
	xorSlice(payload, key)
	xorSlice(payload, key)
	if !bytes.Equal(payload, orig) {
		t.Fatal("XOR must be self-inverse")
	}
	// 循环性：同明文字节相隔 32 位必得同密文（key[i%32] 循环）
	data := make([]byte, 64)
	data[0], data[32] = 0x5A, 0x5A
	xorSlice(data, key)
	if data[0] != data[32] {
		t.Fatal("key must cycle every 32 bytes")
	}
}

// verifiedConn 双向往返 + 分片：写侧产伪 TLS 记录 [23|03 03|4+n|tag|payload]，
// 读侧验链交付；>16384 载荷分片
func TestVerifiedConnRoundTrip(t *testing.T) {
	serverRandom := make([]byte, tlsRandomSize)
	rand.Read(serverRandom)
	password := "secret"

	serverWrite := hmac.New(sha1.New, []byte(password))
	hmacReset(serverWrite, serverRandom, 'S')
	serverRead := hmac.New(sha1.New, []byte(password))
	hmacReset(serverRead, serverRandom, 'C')

	rawServer, rawPeer := net.Pipe()
	defer rawServer.Close()
	defer rawPeer.Close()
	vc := newVerifiedConn(rawServer, serverWrite, serverRead)

	// 对端用链C 构造合法帧
	clientChain := hmac.New(sha1.New, []byte(password))
	hmacReset(clientChain, serverRandom, 'C')
	writeClientFrame := func(payload []byte) {
		header := make([]byte, tlsHMACHeaderSize)
		header[0], header[1], header[2] = recordApplicationData, 3, 3
		binary.BigEndian.PutUint16(header[3:], uint16(hmacSize+len(payload)))
		clientChain.Write(payload)
		tag := append([]byte(nil), clientChain.Sum(nil)[:hmacSize]...)
		clientChain.Write(tag)
		copy(header[tlsHeaderSize:], tag)
		rawPeer.Write(append(header, payload...))
	}

	go writeClientFrame([]byte("ping-from-client"))

	buf := make([]byte, 64)
	n, err := vc.Read(buf)
	if err != nil || string(buf[:n]) != "ping-from-client" {
		t.Fatalf("read failed: %v, got %q", err, buf[:n])
	}

	// 写侧：对端读原始字节，检查记录格式与独立链S 的 tag 逐帧一致
	payload := bytes.Repeat([]byte("Z"), 20000) // 触发 >16384 分片
	writeDone := make(chan error, 1)
	go func() {
		_, err := vc.Write(payload)
		writeDone <- err
	}()
	chainInd := hmac.New(sha1.New, []byte(password))
	hmacReset(chainInd, serverRandom, 'S')
	var reassembled []byte
	header := make([]byte, tlsHeaderSize)
	frames := 0
	for len(reassembled) < len(payload) {
		if _, err := io.ReadFull(rawPeer, header); err != nil {
			t.Fatal(err)
		}
		if header[0] != recordApplicationData || header[1] != 3 || header[2] != 3 {
			t.Fatalf("bad record header %v", header)
		}
		length := int(binary.BigEndian.Uint16(header[3:]))
		if length > hmacSize+maxTLSPlaintext {
			t.Fatalf("record length %d exceeds fragment limit", length)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(rawPeer, body); err != nil {
			t.Fatal(err)
		}
		tag, framePayload := body[:hmacSize], body[hmacSize:]
		chainInd.Write(framePayload)
		wantTag := append([]byte(nil), chainInd.Sum(nil)[:hmacSize]...)
		chainInd.Write(wantTag)
		if !hmac.Equal(tag, wantTag) {
			t.Fatalf("frame %d: write-side tag mismatch against independent chain S", frames)
		}
		reassembled = append(reassembled, framePayload...)
		frames++
	}
	if err := <-writeDone; err != nil {
		t.Fatal("write failed:", err)
	}
	if frames < 2 {
		t.Fatalf("expected fragmentation into >=2 frames, got %d", frames)
	}
	if !bytes.Equal(reassembled, payload) {
		t.Fatal("fragmented payloads do not reassemble to original")
	}
}

// 篡改 HMAC → 服务端回 31 字节随机 alert 并报错（规格 §4.10）
func TestVerifiedConnTamperAlert(t *testing.T) {
	serverRandom := make([]byte, tlsRandomSize)
	rand.Read(serverRandom)
	password := "secret"

	serverWrite := hmac.New(sha1.New, []byte(password))
	hmacReset(serverWrite, serverRandom, 'S')
	serverRead := hmac.New(sha1.New, []byte(password))
	hmacReset(serverRead, serverRandom, 'C')

	rawServer, rawPeer := net.Pipe()
	defer rawServer.Close()
	defer rawPeer.Close()
	vc := newVerifiedConn(rawServer, serverWrite, serverRead)

	header := []byte{recordApplicationData, 3, 3, 0, 4 + 5}
	frame := append(append([]byte{}, header...), 1, 2, 3, 4, 'h', 'e', 'l', 'l', 'o')
	go rawPeer.Write(frame)

	alert := make([]byte, verificationAlertSize)
	alertRead := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(rawPeer, alert)
		alertRead <- err
	}()

	buf := make([]byte, 64)
	if _, err := vc.Read(buf); err == nil {
		t.Fatal("expected verification error on tampered tag")
	}
	if err := <-alertRead; err != nil {
		t.Fatal("no alert received:", err)
	}
	if alert[0] != recordAlert || alert[1] != 3 || alert[2] != 3 {
		t.Fatalf("bad alert record header: %v", alert[:5])
	}
	if binary.BigEndian.Uint16(alert[3:5]) != verificationAlertSize-tlsHeaderSize {
		t.Fatal("bad alert record length")
	}
	if bytes.Equal(alert[tlsHeaderSize:], bytes.Repeat([]byte{0}, verificationAlertSize-tlsHeaderSize)) {
		t.Fatal("alert payload must be random")
	}
}
