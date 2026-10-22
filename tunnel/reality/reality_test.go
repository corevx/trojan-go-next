package reality

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/config"
)

// selfSignedEd25519DER 生成与 REALITY 服务端同款的临时 ed25519 自签证书
// （进程级一次性密钥、空主题），供回执校验测试使用。
func selfSignedEd25519DER(t *testing.T) (ed25519.PublicKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject:      pkix.Name{},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}
	return pub, der
}

// rfc5869Extract/expand 是测试内独立实现的 HKDF（RFC 5869），与被测代码
// 所用的 crypto/hkdf 互为对照，避免同源实现掩盖接线错误。
func rfc5869Extract(salt, ikm []byte) []byte {
	if salt == nil {
		salt = make([]byte, sha256.Size)
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write(ikm)
	return mac.Sum(nil)
}

func rfc5869Expand(prk, info []byte, length int) []byte {
	out := make([]byte, 0, length)
	var t []byte
	for i := byte(1); len(out) < length; i++ {
		mac := hmac.New(sha256.New, prk)
		mac.Write(t)
		mac.Write(info)
		mac.Write([]byte{i})
		t = mac.Sum(nil)
		out = append(out, t...)
	}
	return out[:length]
}

func mustX25519Key(t *testing.T, scalarHex string) *ecdh.PrivateKey {
	t.Helper()
	scalar, err := hex.DecodeString(scalarHex)
	if err != nil || len(scalar) != 32 {
		t.Fatalf("bad test scalar %q", scalarHex)
	}
	// x25519 私钥字节序即标量原值（无 clamp 要求，ecdh 包接受任意 32 字节）
	key, err := ecdh.X25519().NewPrivateKey(scalar)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}
	return key
}

// TestSealSessionIDVector 用固定向量锁定认证材料构造：
//   - authKey == HKDF-SHA256(x25519 共享密钥, salt=Random[:20], info="REALITY")
//     （独立 RFC 5869 实现对照）
//   - session_id 32 字节 = AES-256-GCM 密封的 [版本|保留|时间戳|short-id]
//   - 服务端视角可以原样 Open 复原明文并核对各字段
func TestSealSessionIDVector(t *testing.T) {
	clientKey := mustX25519Key(t, "a546e36bf0527c9d3b16154b82465edd621134c2ac7eac52b5c59b7c3a5b5e0c")
	serverKey := mustX25519Key(t, "4cdd0d24e5a86b2c8bfe2f0e5c7d1e62d2bb16d50e0d7c9f8e2a1b3c4d5e6f70")
	serverPub := serverKey.PublicKey()

	random := bytes.Repeat([]byte{0xAB}, 32)
	rawHello := bytes.Repeat([]byte{0x11}, 96) // 任意 ≥ 71 字节的原始 hello
	var shortID [shortIDLen]byte
	copy(shortID[:], []byte{0xde, 0xad, 0xbe, 0xef})
	now := time.Unix(0x5f5e1000, 0) // 1600000000

	authKey, sessionID, err := sealSessionID(random, rawHello, clientKey, serverPub, shortID, now)
	if err != nil {
		t.Fatalf("sealSessionID: %v", err)
	}

	// 对照一：authKey 派生（独立 HKDF 实现）
	shared, err := clientKey.ECDH(serverPub)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}
	prk := rfc5869Extract(random[:20], shared)
	wantAuthKey := rfc5869Expand(prk, []byte(hkdfInfo), 32)
	if !hmac.Equal(authKey, wantAuthKey) {
		t.Fatalf("authKey mismatch:\n got  %x\n want %x", authKey, wantAuthKey)
	}

	// 对照二：服务端视角解封 session_id，核对明文布局
	block, _ := aes.NewCipher(wantAuthKey)
	aead, _ := cipher.NewGCM(block)
	plaintext, err := aead.Open(nil, random[20:], sessionID, rawHello)
	if err != nil {
		t.Fatalf("server-side Open failed: %v", err)
	}
	if len(plaintext) != 16 {
		t.Fatalf("plaintext length = %d, want 16", len(plaintext))
	}
	if !bytes.Equal(plaintext[0:3], clientVersion[:]) {
		t.Fatalf("version bytes = %x, want %x", plaintext[0:3], clientVersion[:])
	}
	if plaintext[3] != 0 {
		t.Fatalf("reserved byte = %d, want 0", plaintext[3])
	}
	if got := binary.BigEndian.Uint32(plaintext[4:]); got != uint32(now.Unix()) {
		t.Fatalf("timestamp = %d, want %d", got, now.Unix())
	}
	if !bytes.Equal(plaintext[8:], shortID[:]) {
		t.Fatalf("short id = %x, want %x", plaintext[8:], shortID[:])
	}

	// 对照三：AAD 敏感——session_id 区不同的 hello 解密必败（AAD 绑定原始
	// ClientHello，防整体重放/改写）
	tampered := append([]byte{}, rawHello...)
	tampered[50] ^= 0xFF
	if _, err := aead.Open(nil, random[20:], sessionID, tampered); err == nil {
		t.Fatal("Open with tampered AAD unexpectedly succeeded")
	}

	// 对照四：错误服务端公钥派生出的 authKey 必不相同（凭证隔离）
	wrongServer := mustX25519Key(t, "1111111111111111111111111111111111111111111111111111111111111111")
	if wrongShared, _ := clientKey.ECDH(wrongServer.PublicKey()); hmac.Equal(shared, wrongShared) {
		t.Fatal("different server keys derived identical shared secret")
	}
}

// TestSealSessionIDRejectsBadRandom 锁定输入形状校验。
func TestSealSessionIDRejectsBadRandom(t *testing.T) {
	clientKey := mustX25519Key(t, "a546e36bf0527c9d3b16154b82465edd621134c2ac7eac52b5c59b7c3a5b5e0c")
	serverKey := mustX25519Key(t, "4cdd0d24e5a86b2c8bfe2f0e5c7d1e62d2bb16d50e0d7c9f8e2a1b3c4d5e6f70")
	if _, _, err := sealSessionID(make([]byte, 31), make([]byte, 96), clientKey, serverKey.PublicKey(), [8]byte{}, time.Now()); err == nil {
		t.Fatal("short random accepted")
	}
}

// TestVerifierHMACReceipt 锁定服务端回执校验：ed25519 公钥证书 + 签名域
// 携带 HMAC-SHA512(authKey, pub) 时 verified；无回执时不置位且自签证书
// 标准校验必败（回落真站路径表现为握手错误）。
func TestVerifierHMACReceipt(t *testing.T) {
	authKey := bytes.Repeat([]byte{0x42}, 32)
	edPub, der := selfSignedEd25519DER(t)

	v := &verifier{serverName: "www.example.com", authKey: authKey}
	h := hmac.New(sha512.New, authKey)
	h.Write(edPub)
	copy(der[len(der)-64:], h.Sum(nil))

	if err := v.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Fatalf("VerifyPeerCertificate with receipt: %v", err)
	}
	if !v.verified {
		t.Fatal("receipt not recognized")
	}

	// 无回执（authKey 不匹配）：verified 保持 false，且自签证书走标准校验
	// 必败——即错误凭证场景下握手会以证书错误终止。
	v2 := &verifier{serverName: "www.example.com", authKey: bytes.Repeat([]byte{0x00}, 32)}
	if err := v2.VerifyPeerCertificate([][]byte{der}, nil); err == nil {
		t.Fatal("self-signed cert without receipt must fail standard verification")
	}
	if v2.verified {
		t.Fatal("mismatched receipt must not verify")
	}
}

// TestNewClientConfigValidation 锁定配置校验：公钥/short-id 解码与长度门槛。
func TestNewClientConfigValidation(t *testing.T) {
	validPub := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x33}, 32))
	cases := []struct {
		name    string
		pub     string
		shortID string
		wantErr bool
	}{
		{"valid", validPub, "0123456789abcdef", false},
		{"empty short id", validPub, "", false},
		{"short id 4 bytes", validPub, "deadbeef", false},
		{"short id too long", validPub, "0123456789abcdef00", true},
		{"short id bad hex", validPub, "zz", true},
		{"pub not base64", "!!!", "", true},
		{"pub wrong length", "YWJj", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := config.WithConfig(context.Background(), Name, &Config{
				RemoteHost: "example.com",
				Reality: RealityConfig{
					Enabled:     true,
					PublicKey:   c.pub,
					ShortID:     c.shortID,
					ServerName:  "www.example.com",
					Fingerprint: "chrome",
				},
			})
			_, err := NewClient(ctx, nil)
			if (err != nil) != c.wantErr {
				t.Fatalf("NewClient err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}

	// server-name 缺省回落到 remote_addr
	ctx := config.WithConfig(context.Background(), Name, &Config{
		RemoteHost: "fallback.example.com",
		Reality: RealityConfig{
			Enabled:     true,
			PublicKey:   validPub,
			Fingerprint: "chrome",
		},
	})
	cl, err := NewClient(ctx, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if cl.serverName != "fallback.example.com" {
		t.Fatalf("serverName = %q, want fallback to remote_addr", cl.serverName)
	}
	if cl.shortID != ([8]byte{}) {
		t.Fatalf("empty short-id must be zero-filled, got %x", cl.shortID)
	}

	// 非法指纹
	ctx = config.WithConfig(context.Background(), Name, &Config{
		Reality: RealityConfig{Enabled: true, PublicKey: validPub, Fingerprint: "ie"},
	})
	if _, err := NewClient(ctx, nil); err == nil {
		t.Fatal("invalid fingerprint accepted")
	}
}
