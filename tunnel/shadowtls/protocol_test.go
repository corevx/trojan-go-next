package shadowtls

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestReadFrameRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	payload := bytes.Repeat([]byte{0xAB}, 300)
	frame := make([]byte, tlsHeaderSize+len(payload))
	frame[0] = recordApplicationData
	frame[1], frame[2] = 3, 3
	binary.BigEndian.PutUint16(frame[3:], uint16(len(payload)))
	copy(frame[tlsHeaderSize:], payload)

	go a.Write(frame)
	got, err := readFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatal("frame round trip mismatch")
	}

	// 半帧：只写头与前几字节，应阻塞而非返回错误帧
	go func() {
		a.Write(frame[:tlsHeaderSize+3])
	}()
	_ = b.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := readFrame(b); err == nil {
		t.Fatal("expected timeout on partial frame")
	}
}

func TestExtractServerName(t *testing.T) {
	frame := craftClientHello("www.example.com")
	sni, err := extractServerName(frame)
	if err != nil {
		t.Fatal(err)
	}
	if sni != "www.example.com" {
		t.Fatalf("expected www.example.com, got %s", sni)
	}

	// 无 SNI 扩展的 ClientHello → 报错但不 panic
	noSNI := craftClientHelloNoExt()
	if _, err := extractServerName(noSNI); err == nil {
		t.Fatal("expected error for ClientHello without SNI")
	}
}

func craftClientHelloNoExt() []byte {
	frame := craftClientHello("x")
	// 直接截掉 server_name 扩展：把扩展长度改 0
	// 布局：... compression(1) + ext_len(2) + ext
	frame[len(frame)-2] = 0
	frame[len(frame)-1] = 0
	return frame[:len(frame)-2-9-len("x")-4] // 粗截断即可，解析路径一致
}

func TestExtractServerRandom(t *testing.T) {
	sh := craftServerHello(true)
	random := extractServerRandom(sh)
	if random == nil {
		t.Fatal("failed to extract server random")
	}
	if !bytes.Equal(random, sh[serverRandomIndex:serverRandomIndex+tlsRandomSize]) {
		t.Fatal("server random mismatch")
	}

	// 非 ServerHello → nil
	ch := craftClientHello("www.example.com")
	if extractServerRandom(ch) != nil {
		t.Fatal("expected nil for ClientHello")
	}
}

func TestIsServerHelloTLS13(t *testing.T) {
	sh13 := craftServerHello(true)
	if !isServerHelloTLS13(sh13) {
		t.Fatal("TLS 1.3 ServerHello not detected")
	}
	sh12 := craftServerHello(false)
	if isServerHelloTLS13(sh12) {
		t.Fatal("TLS 1.2 ServerHello wrongly detected as 1.3")
	}
}

// craftServerHello 构造 ServerHello 记录。tls13 时带 supported_versions
// 扩展取值 0x0304；否则无扩展（legacy_version=0x0303 即 TLS1.2 语义）。
func craftServerHello(tls13 bool) []byte {
	body := make([]byte, 0, 96)
	body = binary.BigEndian.AppendUint16(body, 0x0303)
	random := make([]byte, tlsRandomSize)
	for i := range random {
		random[i] = byte(i + 1)
	}
	body = append(body, random...)
	body = append(body, tlsSessionIDSize) // echo session id
	for i := 0; i < tlsSessionIDSize; i++ {
		body = append(body, byte(0x42))
	}
	body = binary.BigEndian.AppendUint16(body, 0x1301) // cipher suite
	body = append(body, 0)                             // compression
	if tls13 {
		body = binary.BigEndian.AppendUint16(body, 2+2+2) // ext total len
		body = binary.BigEndian.AppendUint16(body, 43)    // supported_versions
		body = binary.BigEndian.AppendUint16(body, 2)
		body = binary.BigEndian.AppendUint16(body, 0x0304)
	}

	frame := make([]byte, 0, tlsHeaderSize+4+len(body))
	frame = append(frame, recordHandshake, 3, 3)
	frame = binary.BigEndian.AppendUint16(frame, uint16(4+len(body)))
	frame = append(frame, handshakeServerHello)
	frame = append(frame, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	frame = append(frame, body...)
	return frame
}
