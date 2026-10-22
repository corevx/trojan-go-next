// Package shadowtls 实现 ShadowTLS v3 协议的服务端层：对外端口呈现与真站
// 完全一致的 TLS 握手（中继真站流量并单侧改写），鉴权凭据藏在 ClientHello
// 的 session_id 内（28 字节随机 + HMAC-SHA1 截断 4 字节），鉴权后连接劫持
// 为逐帧 HMAC 链保护的伪 TLS 记录管道，内层承载裸 trojan 协议。
//
// 协议规格源自 ShadowTLS v3（ihciah/shadow-tls，MIT/Apache-2.0），并经
// GPL-3.0 实现 mihomo / sing-shadowtls 交叉验证（见 demos/07 规格文档）；
// 本实现按规格独立重写，非逐行翻译。
package shadowtls

import (
	"encoding/binary"
	"io"
	"net"

	"github.com/p4gefau1t/trojan-go/common"
)

const (
	tlsHeaderSize         = 5
	tlsRandomSize         = 32
	tlsSessionIDSize      = 32
	hmacSize              = 4 // HMAC-SHA1 截断长度
	tlsHMACHeaderSize     = tlsHeaderSize + hmacSize
	maxTLSPlaintext       = 16384
	verificationAlertSize = 31

	recordChangeCipherSpec = 20
	recordAlert            = 21
	recordHandshake        = 22
	recordApplicationData  = 23

	handshakeClientHello = 1
	handshakeServerHello = 2

	handshakeHeaderSize = 4 // type(1) + length(3)

	// 相对 TLS 记录（含 5 字节记录头）的字节偏移，推导见规格 §3：
	// record header(5) + handshake type(1) + length(3) + legacy_version(2)
	serverRandomIndex = tlsHeaderSize + 1 + 3 + 2 // ServerHello random 起点
	// record header(5) + handshake type(1) + length(3) + legacy_version(2) + random(32)
	sessionIDLengthIndex = tlsHeaderSize + 1 + 3 + 2 + tlsRandomSize // ClientHello session_id 长度字节
	// session_id 本体 = frame[sessionIDLengthIndex+1 : +1+32]，其中 HMAC 位于末 4 字节
)

// readFrame 完整读取一个 TLS 记录（含 5 字节头）。协议要求逐帧处理，
// 禁止流式半帧，见规格 §5。
func readFrame(reader io.Reader) ([]byte, error) {
	var header [tlsHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[3:]))
	frame := make([]byte, tlsHeaderSize+length)
	copy(frame, header[:])
	if _, err := io.ReadFull(reader, frame[tlsHeaderSize:]); err != nil {
		return nil, err
	}
	return frame, nil
}

// extractServerName 从 ClientHello 记录中提取 SNI。提取失败不阻断流程
// （验签失败时仍可走默认真站回退），由调用方决定如何处理错误。
func extractServerName(frame []byte) (string, error) {
	if len(frame) < tlsHeaderSize+handshakeHeaderSize || frame[0] != recordHandshake || frame[tlsHeaderSize] != handshakeClientHello {
		return "", common.NewError("invalid ClientHello record")
	}
	helloLength := int(frame[tlsHeaderSize+1])<<16 | int(frame[tlsHeaderSize+2])<<8 | int(frame[tlsHeaderSize+3])
	hello := frame[tlsHeaderSize+handshakeHeaderSize:]
	if helloLength > len(hello) {
		return "", common.NewError("truncated ClientHello body")
	}
	hello = hello[:helloLength]
	if len(hello) < 2+tlsRandomSize+1 {
		return "", common.NewError("truncated ClientHello body")
	}
	offset := 2 + tlsRandomSize
	sessionIDLength := int(hello[offset])
	offset++
	if offset+sessionIDLength+2 > len(hello) {
		return "", common.NewError("truncated ClientHello session id")
	}
	offset += sessionIDLength
	cipherSuitesLength := int(binary.BigEndian.Uint16(hello[offset:]))
	offset += 2
	if offset+cipherSuitesLength+1 > len(hello) {
		return "", common.NewError("truncated ClientHello cipher suites")
	}
	offset += cipherSuitesLength
	compressionMethodsLength := int(hello[offset])
	offset++
	if offset+compressionMethodsLength+2 > len(hello) {
		return "", common.NewError("ClientHello has no SNI")
	}
	offset += compressionMethodsLength
	extensionsLength := int(binary.BigEndian.Uint16(hello[offset:]))
	offset += 2
	if offset+extensionsLength > len(hello) {
		return "", common.NewError("truncated ClientHello extensions")
	}
	extensions := hello[offset : offset+extensionsLength]
	for len(extensions) >= 4 {
		extensionType := binary.BigEndian.Uint16(extensions)
		extensionLength := int(binary.BigEndian.Uint16(extensions[2:]))
		extensions = extensions[4:]
		if extensionLength > len(extensions) {
			return "", common.NewError("truncated ClientHello extension")
		}
		if extensionType == 0 {
			return parseServerNameExtension(extensions[:extensionLength])
		}
		extensions = extensions[extensionLength:]
	}
	return "", common.NewError("ClientHello has no SNI")
}

func parseServerNameExtension(extension []byte) (string, error) {
	if len(extension) < 2 {
		return "", common.NewError("truncated server_name extension")
	}
	listLength := int(binary.BigEndian.Uint16(extension))
	if listLength > len(extension)-2 {
		return "", common.NewError("truncated server_name list")
	}
	list := extension[2 : 2+listLength]
	for len(list) >= 3 {
		nameType := list[0]
		nameLength := int(binary.BigEndian.Uint16(list[1:]))
		list = list[3:]
		if nameLength > len(list) {
			return "", common.NewError("truncated server_name entry")
		}
		if nameType == 0 && nameLength > 0 {
			return string(list[:nameLength]), nil
		}
		list = list[nameLength:]
	}
	return "", common.NewError("ClientHello has no host name")
}

// extractServerRandom 提取 ServerHello 记录的 32 字节 random。
// 仅认第一个 ServerHello 帧（HRR 场景由调用方保证只传首帧，见规格 §4.8）。
func extractServerRandom(frame []byte) []byte {
	if len(frame) < serverRandomIndex+tlsRandomSize || frame[0] != recordHandshake || frame[tlsHeaderSize] != handshakeServerHello {
		return nil
	}
	return append([]byte(nil), frame[serverRandomIndex:serverRandomIndex+tlsRandomSize]...)
}

// isServerHelloTLS13 判断 ServerHello 是否协商了 TLS 1.3：
// supported_versions(0x2B) 扩展取值 0x0304，见规格 §4.6。
func isServerHelloTLS13(frame []byte) bool {
	if len(frame) <= sessionIDLengthIndex || frame[0] != recordHandshake || frame[tlsHeaderSize] != handshakeServerHello {
		return false
	}
	offset := sessionIDLengthIndex
	sessionIDLength := int(frame[offset])
	offset++
	// cipher_suite(2) + compression(1)
	if offset+sessionIDLength+3+2 > len(frame) {
		return false
	}
	offset += sessionIDLength + 3
	extensionsLength := int(binary.BigEndian.Uint16(frame[offset:]))
	offset += 2
	if offset+extensionsLength > len(frame) {
		return false
	}
	extensions := frame[offset : offset+extensionsLength]
	for len(extensions) >= 4 {
		extensionType := binary.BigEndian.Uint16(extensions)
		extensionLength := int(binary.BigEndian.Uint16(extensions[2:]))
		extensions = extensions[4:]
		if extensionLength > len(extensions) {
			return false
		}
		if extensionType == 43 {
			return extensionLength == 2 && binary.BigEndian.Uint16(extensions[:2]) == 0x0304
		}
		extensions = extensions[extensionLength:]
	}
	return false
}

// writeBuffers 聚合多次小写入为一次系统调用，减少伪记录头 + HMAC + 载荷
// 三段数据被拆开发送造成的可观测行为差异
func writeBuffers(writer io.Writer, buffers ...[]byte) error {
	netBuffers := net.Buffers(buffers)
	_, err := netBuffers.WriteTo(writer)
	return err
}
