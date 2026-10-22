package scenario

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"testing"
	"time"

	netproxy "golang.org/x/net/proxy"

	_ "github.com/p4gefau1t/trojan-go/api"
	_ "github.com/p4gefau1t/trojan-go/api/service"
	"github.com/p4gefau1t/trojan-go/common"
	_ "github.com/p4gefau1t/trojan-go/log/golog"
	"github.com/p4gefau1t/trojan-go/proxy"
	_ "github.com/p4gefau1t/trojan-go/proxy/client"
	_ "github.com/p4gefau1t/trojan-go/proxy/forward"
	_ "github.com/p4gefau1t/trojan-go/proxy/nat"
	_ "github.com/p4gefau1t/trojan-go/proxy/server"
	_ "github.com/p4gefau1t/trojan-go/statistic/memory"
	"github.com/p4gefau1t/trojan-go/test/util"
)

// generate a self-signed cert at test runtime so it never expires
func genTestCert() (certPEM, keyPEM []byte) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	common.Must(err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	common.Must(err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return
}

func init() {
	certPEM, keyPEM := genTestCert()
	os.WriteFile("server.crt", certPEM, 0o600)
	os.WriteFile("server.key", keyPEM, 0o600)
}

func CheckClientServer(clientData, serverData string, socksPort int) (ok bool) {
	server, err := proxy.NewProxyFromConfigData([]byte(serverData), false)
	common.Must(err)
	go server.Run()

	client, err := proxy.NewProxyFromConfigData([]byte(clientData), false)
	common.Must(err)
	go client.Run()

	time.Sleep(time.Second * 2)
	dialer, err := netproxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, netproxy.Direct)
	common.Must(err)

	ok = true
	const num = 100
	wg := sync.WaitGroup{}
	wg.Add(num)
	for i := 0; i < num; i++ {
		go func() {
			const payloadSize = 1024
			payload := util.GeneratePayload(payloadSize)
			buf := [payloadSize]byte{}

			conn, err := dialer.Dial("tcp", util.EchoAddr)
			common.Must(err)

			common.Must2(conn.Write(payload))
			common.Must2(conn.Read(buf[:]))

			if !bytes.Equal(payload, buf[:]) {
				ok = false
			}
			conn.Close()
			wg.Done()
		}()
	}
	wg.Wait()
	client.Close()
	server.Close()
	return
}

func TestClientServerWebsocketSubTree(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")
	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
websocket:
    enabled: true
    path: /ws
    host: somedomainname.com
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
`, socksPort, serverPort)
	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
disable-http-check: true
password:
    - password
ssl:
    verify-hostname: false
    key: server.key
    cert: server.crt
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
websocket:
    enabled: true
    path: /ws
    host: 127.0.0.1
`, serverPort, util.HTTPPort)

	if !CheckClientServer(clientData, serverData, socksPort) {
		t.Fail()
	}
}

func TestClientServerTrojanSubTree(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")
	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
`, socksPort, serverPort)
	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
disable-http-check: true
password:
    - password
ssl:
    verify-hostname: false
    key: server.key
    cert: server.crt
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
`, serverPort, util.HTTPPort)

	if !CheckClientServer(clientData, serverData, socksPort) {
		t.Fail()
	}
}

func TestWebsocketDetection(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")

	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
`, socksPort, serverPort)
	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
disable-http-check: true
password:
    - password
ssl:
    verify-hostname: false
    key: server.key
    cert: server.crt
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
websocket:
    enabled: true
    path: /ws
    hostname: 127.0.0.1
`, serverPort, util.HTTPPort)

	if !CheckClientServer(clientData, serverData, socksPort) {
		t.Fail()
	}
}

func TestPluginWebsocket(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")

	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
password:
    - password
transport-plugin:
    enabled: true
    type: plaintext
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
websocket:
    enabled: true
    path: /ws
    hostname: 127.0.0.1
`, socksPort, serverPort)
	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
disable-http-check: true
password:
    - password
transport-plugin:
    enabled: true
    type: plaintext
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
websocket:
    enabled: true
    path: /ws
    hostname: 127.0.0.1
`, serverPort, util.HTTPPort)

	if !CheckClientServer(clientData, serverData, socksPort) {
		t.Fail()
	}
}

func TestForward(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	clientPort := common.PickPort("tcp", "127.0.0.1")
	_, targetPort, _ := net.SplitHostPort(util.EchoAddr)
	clientData := fmt.Sprintf(`
run-type: forward
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
target-addr: 127.0.0.1
target-port: %s
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
websocket:
    enabled: true
    path: /ws
    hostname: 127.0.0.1
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
`, clientPort, serverPort, targetPort)
	go func() {
		proxy, err := proxy.NewProxyFromConfigData([]byte(clientData), false)
		common.Must(err)
		common.Must(proxy.Run())
	}()

	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
disable-http-check: true
password:
    - password
ssl:
    verify-hostname: false
    key: server.key
    cert: server.crt
    sni: "localhost"
websocket:
    enabled: true
    path: /ws
    hostname: 127.0.0.1
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
`, serverPort, util.HTTPPort)
	go func() {
		proxy, err := proxy.NewProxyFromConfigData([]byte(serverData), false)
		common.Must(err)
		common.Must(proxy.Run())
	}()

	time.Sleep(time.Second * 2)

	payload := util.GeneratePayload(1024)
	buf := [1024]byte{}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", clientPort))
	common.Must(err)

	common.Must2(conn.Write(payload))
	common.Must2(conn.Read(buf[:]))

	if !bytes.Equal(payload, buf[:]) {
		t.Fail()
	}

	packet, err := net.ListenPacket("udp", "")
	common.Must(err)
	common.Must2(packet.WriteTo(payload, &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: clientPort,
	}))
	_, _, err = packet.ReadFrom(buf[:])
	common.Must(err)
	if !bytes.Equal(payload, buf[:]) {
		t.Fail()
	}
}

func TestLeak(t *testing.T) {
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")
	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
log-level: 0
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
shadowsocks:
    enabled: true
    method: AEAD_CHACHA20_POLY1305
    password: 12345678
mux:
    enabled: true
api:
    enabled: true
    api-port: 0
`, socksPort, serverPort)
	client, err := proxy.NewProxyFromConfigData([]byte(clientData), false)
	common.Must(err)
	go client.Run()
	time.Sleep(time.Second * 3)
	client.Close()
	time.Sleep(time.Second * 3)
	// http.ListenAndServe("localhost:6060", nil)
}

func SingleThreadBenchmark(clientData, serverData string, socksPort int) {
	server, err := proxy.NewProxyFromConfigData([]byte(clientData), false)
	common.Must(err)
	go server.Run()

	client, err := proxy.NewProxyFromConfigData([]byte(serverData), false)
	common.Must(err)
	go client.Run()

	time.Sleep(time.Second * 2)
	dialer, err := netproxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, netproxy.Direct)
	common.Must(err)

	const num = 100
	wg := sync.WaitGroup{}
	wg.Add(num)
	const payloadSize = 1024 * 1024 * 1024
	payload := util.GeneratePayload(payloadSize)

	for i := 0; i < 100; i++ {
		conn, err := dialer.Dial("tcp", util.BlackHoleAddr)
		common.Must(err)

		t1 := time.Now()
		common.Must2(conn.Write(payload))
		t2 := time.Now()

		speed := float64(payloadSize) / (float64(t2.Sub(t1).Nanoseconds()) / float64(time.Second))
		fmt.Printf("speed: %f Gbps\n", speed/1024/1024/1024)

		conn.Close()
	}
	client.Close()
	server.Close()
}

func BenchmarkClientServer(b *testing.B) {
	go func() {
		fmt.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	serverPort := common.PickPort("tcp", "127.0.0.1")
	socksPort := common.PickPort("tcp", "127.0.0.1")
	clientData := fmt.Sprintf(`
run-type: client
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %d
log-level: 0
password:
    - password
ssl:
    verify: false
    fingerprint: firefox
    sni: localhost
`, socksPort, serverPort)
	serverData := fmt.Sprintf(`
run-type: server
local-addr: 127.0.0.1
local-port: %d
remote-addr: 127.0.0.1
remote-port: %s
log-level: 0
disable-http-check: true
password:
    - password
ssl:
    verify-hostname: false
    key: server.key
    cert: server.crt
    sni: localhost
`, serverPort, util.HTTPPort)

	SingleThreadBenchmark(clientData, serverData, socksPort)
}
