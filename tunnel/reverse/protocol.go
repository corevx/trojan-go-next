package reverse

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/p4gefau1t/trojan-go/common"
)

// protoVersion is the version byte of every reverse control frame
const protoVersion byte = 0x01

// frame types carried on the control stream
const (
	frameBindReq   byte = 0x01
	frameBindResp  byte = 0x02
	framePing      byte = 0x03
	framePong      byte = 0x04
	frameUnbindReq byte = 0x05
)

// BindResp codes
const (
	CodeOK       byte = 0
	CodeACL      byte = 1
	CodeInUse    byte = 2
	CodeBadParam byte = 3
	CodeLimit    byte = 4
)

const maxFramePayload = 4096

// Frame layout: ver(1B) + type(1B) + length(2B, big endian) + payload

func writeFrame(w io.Writer, frameType byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return common.NewError("reverse frame payload too large")
	}
	buf := bytes.NewBuffer(make([]byte, 0, len(payload)+4))
	buf.WriteByte(protoVersion)
	buf.WriteByte(frameType)
	header := [2]byte{}
	binary.BigEndian.PutUint16(header[:], uint16(len(payload)))
	buf.Write(header[:])
	buf.Write(payload)
	return common.WriteAllBytes(w, buf.Bytes())
}

func readFrame(r io.Reader) (frameType byte, payload []byte, err error) {
	header := [4]byte{}
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, common.NewError("failed to read reverse frame header").Base(err)
	}
	if header[0] != protoVersion {
		return 0, nil, common.NewError("unknown reverse protocol version")
	}
	length := int(binary.BigEndian.Uint16(header[2:4]))
	if length > maxFramePayload {
		return 0, nil, common.NewError("reverse frame too large")
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, common.NewError("failed to read reverse frame payload").Base(err)
	}
	return header[1], payload, nil
}

// bindReq asks the server to bind a port: port u16 + flags u8 + reserved u8
type bindReq struct {
	Port     int
	Flags    byte
	Reserved byte
}

func (m *bindReq) marshal() []byte {
	payload := [4]byte{}
	binary.BigEndian.PutUint16(payload[0:2], uint16(m.Port))
	payload[2] = m.Flags
	payload[3] = m.Reserved
	return payload[:]
}

func parseBindReq(payload []byte) (*bindReq, error) {
	if len(payload) != 4 {
		return nil, common.NewError("malformed reverse bind request")
	}
	return &bindReq{
		Port:     int(binary.BigEndian.Uint16(payload[0:2])),
		Flags:    payload[2],
		Reserved: payload[3],
	}, nil
}

// bindResp answers a bindReq: code u8 + port u16 + variable-length UTF-8 message
type bindResp struct {
	Code byte
	Port int
	Msg  string
}

func (m *bindResp) marshal() []byte {
	buf := bytes.NewBuffer(make([]byte, 0, len(m.Msg)+3))
	buf.WriteByte(m.Code)
	port := [2]byte{}
	binary.BigEndian.PutUint16(port[:], uint16(m.Port))
	buf.Write(port[:])
	buf.WriteString(m.Msg)
	return buf.Bytes()
}

func parseBindResp(payload []byte) (*bindResp, error) {
	if len(payload) < 3 {
		return nil, common.NewError("malformed reverse bind response")
	}
	return &bindResp{
		Code: payload[0],
		Port: int(binary.BigEndian.Uint16(payload[1:3])),
		Msg:  string(payload[3:]),
	}, nil
}

type ping struct {
	Nonce uint64
}

func (m *ping) marshal() []byte {
	payload := [8]byte{}
	binary.BigEndian.PutUint64(payload[:], m.Nonce)
	return payload[:]
}

func parsePing(payload []byte) (*ping, error) {
	if len(payload) != 8 {
		return nil, common.NewError("malformed reverse ping")
	}
	return &ping{Nonce: binary.BigEndian.Uint64(payload)}, nil
}

type pong struct {
	Nonce uint64
}

func (m *pong) marshal() []byte {
	payload := [8]byte{}
	binary.BigEndian.PutUint64(payload[:], m.Nonce)
	return payload[:]
}

func parsePong(payload []byte) (*pong, error) {
	if len(payload) != 8 {
		return nil, common.NewError("malformed reverse pong")
	}
	return &pong{Nonce: binary.BigEndian.Uint64(payload)}, nil
}

type unbindReq struct {
	Port int
}

func (m *unbindReq) marshal() []byte {
	payload := [2]byte{}
	binary.BigEndian.PutUint16(payload[:], uint16(m.Port))
	return payload[:]
}

func parseUnbindReq(payload []byte) (*unbindReq, error) {
	if len(payload) != 2 {
		return nil, common.NewError("malformed reverse unbind request")
	}
	return &unbindReq{Port: int(binary.BigEndian.Uint16(payload))}, nil
}
