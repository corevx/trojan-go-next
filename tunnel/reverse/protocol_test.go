package reverse

import (
	"bytes"
	"testing"

	"github.com/p4gefau1t/trojan-go/common"
)

func TestReverseProtocol(t *testing.T) {
	buf := &bytes.Buffer{}
	req := &bindReq{Port: 27061, Flags: 1, Reserved: 2}
	common.Must(writeFrame(buf, frameBindReq, req.marshal()))
	frameType, payload, err := readFrame(buf)
	common.Must(err)
	if frameType != frameBindReq {
		t.Fatal("bind request frame type mismatch")
	}
	parsedReq, err := parseBindReq(payload)
	common.Must(err)
	if *parsedReq != *req {
		t.Fatal("bind request round trip mismatch")
	}

	resp := &bindResp{Code: CodeInUse, Port: 27061, Msg: "端口占用"}
	common.Must(writeFrame(buf, frameBindResp, resp.marshal()))
	frameType, payload, err = readFrame(buf)
	common.Must(err)
	if frameType != frameBindResp {
		t.Fatal("bind response frame type mismatch")
	}
	parsedResp, err := parseBindResp(payload)
	common.Must(err)
	if parsedResp.Code != CodeInUse || parsedResp.Port != 27061 || parsedResp.Msg != resp.Msg {
		t.Fatal("bind response round trip mismatch")
	}

	p := &ping{Nonce: 0x0123456789abcdef}
	common.Must(writeFrame(buf, framePing, p.marshal()))
	_, payload, err = readFrame(buf)
	common.Must(err)
	parsedPing, err := parsePing(payload)
	common.Must(err)
	if parsedPing.Nonce != p.Nonce {
		t.Fatal("ping round trip mismatch")
	}

	po := &pong{Nonce: 42}
	common.Must(writeFrame(buf, framePong, po.marshal()))
	_, payload, err = readFrame(buf)
	common.Must(err)
	parsedPong, err := parsePong(payload)
	common.Must(err)
	if parsedPong.Nonce != po.Nonce {
		t.Fatal("pong round trip mismatch")
	}

	u := &unbindReq{Port: 27061}
	common.Must(writeFrame(buf, frameUnbindReq, u.marshal()))
	_, payload, err = readFrame(buf)
	common.Must(err)
	parsedUnbind, err := parseUnbindReq(payload)
	common.Must(err)
	if parsedUnbind.Port != u.Port {
		t.Fatal("unbind request round trip mismatch")
	}

	// a wrong version byte must be rejected
	buf.Reset()
	buf.Write([]byte{0x02, frameBindReq, 0, 0})
	if _, _, err := readFrame(buf); err == nil {
		t.Fatal("unknown protocol version accepted")
	}
}
