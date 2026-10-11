package reverse_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/reverse"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
	"github.com/p4gefau1t/trojan-go/tunnel/trojan"
)

const (
	authPassword     = "password1"
	unauthPassword   = "password2"
	testStreamBuffer = 262144
	testIdleTimeout  = 5
)

// testStack wires an in-process transport+trojan+reverse stack without TLS,
// following the pattern of tunnel/trojan/trojan_test.go.
type testStack struct {
	cancel       context.CancelFunc
	closers      []io.Closer
	ports        []int
	outsidePort  int // free port deliberately NOT in allow-ports
	reverse      *reverse.Server
	trojanServer *trojan.Server
	// authorized is a reverse user (in reverse.users)
	authorized *reverse.Client
	// unauthorized is a valid trojan user which is NOT a reverse user
	unauthorized *reverse.Client
	// plainTrojan is the unauthorized user's trojan client, for proxy checks
	plainTrojan *trojan.Client
}

func (s *testStack) close(t *testing.T) {
	t.Helper()
	for i := len(s.closers) - 1; i >= 0; i-- {
		s.closers[i].Close()
	}
	s.cancel()
}

// pickFreePorts returns n distinct ports that were free at pick time
func pickFreePorts(n int) []int {
	ports := make([]int, 0, n)
	seen := make(map[int]struct{})
	for len(ports) < n {
		port := common.PickPort("tcp", "127.0.0.1")
		if port == 0 {
			continue
		}
		if _, dup := seen[port]; dup {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	return ports
}

func newTestStack(t *testing.T) *testStack {
	t.Helper()
	transportPort := common.PickPort("tcp", "127.0.0.1")
	baseCtx, cancel := context.WithCancel(context.Background())
	baseCtx = config.WithConfig(baseCtx, transport.Name, &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  transportPort,
		RemoteHost: "127.0.0.1",
		RemotePort: transportPort,
	})
	baseCtx = config.WithConfig(baseCtx, freedom.Name, &freedom.Config{})
	tcpServer, err := transport.NewServer(baseCtx, nil)
	common.Must(err)
	tcpClient, err := transport.NewClient(baseCtx, nil)
	common.Must(err)

	ports := pickFreePorts(24)
	// "443" is deliberately allowed but must still be refused as a privileged
	// port; every other entry is an exact single port the tests may bind
	allowPorts := make([]string, 0, len(ports)+1)
	allowPorts = append(allowPorts, "443")
	for _, port := range ports {
		allowPorts = append(allowPorts, fmt.Sprintf("%d", port))
	}
	newReverseConfig := func() *reverse.Config {
		return &reverse.Config{
			Reverse: reverse.Section{
				Enabled:         true,
				DefaultBindHost: "127.0.0.1",
				IdleTimeout:     testIdleTimeout,
				StreamBuffer:    testStreamBuffer,
				Users: []reverse.UserConfig{
					{
						Password:     authPassword,
						AllowPorts:   allowPorts,
						MaxListeners: 2,
					},
				},
			},
		}
	}

	serverCtx := config.WithConfig(baseCtx, memory.Name, &memory.Config{
		Passwords: []string{authPassword, unauthPassword},
	})
	serverCtx = config.WithConfig(serverCtx, trojan.Name, &trojan.Config{
		RemoteHost:       "127.0.0.1",
		RemotePort:       util.EchoPort,
		DisableHTTPCheck: true,
	})
	serverCtx = config.WithConfig(serverCtx, reverse.Name, newReverseConfig())
	trojanServer, err := trojan.NewServer(serverCtx, tcpServer)
	common.Must(err)
	reverseServer, err := reverse.NewServer(serverCtx, trojanServer)
	common.Must(err)

	trojanClientConfig := &trojan.Config{
		RemoteHost: "127.0.0.1",
		RemotePort: transportPort,
	}

	// one context per identity so the trojan client picks a deterministic user
	authCtx := config.WithConfig(baseCtx, memory.Name, &memory.Config{Passwords: []string{authPassword}})
	authCtx = config.WithConfig(authCtx, trojan.Name, trojanClientConfig)
	authCtx = config.WithConfig(authCtx, reverse.Name, newReverseConfig())
	trojanClient1, err := trojan.NewClient(authCtx, tcpClient)
	common.Must(err)
	authorized, err := reverse.NewClient(authCtx, trojanClient1)
	common.Must(err)

	unauthCtx := config.WithConfig(baseCtx, memory.Name, &memory.Config{Passwords: []string{unauthPassword}})
	unauthCtx = config.WithConfig(unauthCtx, trojan.Name, trojanClientConfig)
	unauthCtx = config.WithConfig(unauthCtx, reverse.Name, newReverseConfig())
	plainTrojan, err := trojan.NewClient(unauthCtx, tcpClient)
	common.Must(err)
	unauthorized, err := reverse.NewClient(unauthCtx, plainTrojan)
	common.Must(err)

	return &testStack{
		cancel:       cancel,
		closers:      []io.Closer{tcpServer, trojanServer, reverseServer, trojanClient1, authorized, plainTrojan, unauthorized},
		ports:        ports,
		outsidePort:  pickFreePorts(1)[0],
		reverse:      reverseServer,
		trojanServer: trojanServer,
		authorized:   authorized,
		unauthorized: unauthorized,
		plainTrojan:  plainTrojan,
	}
}

func expectBindCode(t *testing.T, err error, code byte) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected bind rejection code=%d, got success", code)
	}
	var be *reverse.BindError
	if !errors.As(err, &be) {
		t.Fatalf("expected a bind rejection, got %v", err)
	}
	if be.Code != code {
		t.Fatalf("expected bind code=%d, got code=%d (%s)", code, be.Code, be.Msg)
	}
}

// echoOnce dials the server-side bound port and verifies one echo round trip
func echoOnce(t *testing.T, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	payload := util.GeneratePayload(1024)
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(payload); err != nil {
		return false
	}
	recv := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, recv); err != nil {
		return false
	}
	return bytes.Equal(payload, recv)
}

func TestReverseRoundTrip(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	port := stack.ports[0]
	listener, err := stack.authorized.DialReverse(context.Background(), port, reverse.WithTarget(util.EchoAddr))
	common.Must(err)
	defer listener.Close()

	const concurrency = 50
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errs := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			payload := util.GeneratePayload(1024)
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			if _, err := conn.Write(payload); err != nil {
				errs <- err
				return
			}
			recv := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, recv); err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(payload, recv) {
				errs <- fmt.Errorf("echo mismatch on connection %d", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestReverseACL(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	// authorized user, port outside the allow list
	_, err := stack.authorized.DialReverse(context.Background(), stack.outsidePort)
	expectBindCode(t, err, reverse.CodeACL)

	// privileged ports are always refused, even when explicitly allowed
	_, err = stack.authorized.DialReverse(context.Background(), 443)
	expectBindCode(t, err, reverse.CodeACL)

	// a valid trojan user outside reverse.users is refused for reverse
	_, err = stack.unauthorized.DialReverse(context.Background(), stack.ports[5])
	expectBindCode(t, err, reverse.CodeACL)

	// ... but its normal proxy usage is unaffected
	conn1, err := stack.plainTrojan.DialConn(&tunnel.Address{
		DomainName:  "example.com",
		AddressType: tunnel.DomainName,
		Port:        80,
	}, nil)
	common.Must(err)
	defer conn1.Close()
	conn2, err := stack.trojanServer.AcceptConn(nil)
	common.Must(err)
	defer conn2.Close()
	if !util.CheckConn(conn1, conn2) {
		t.Fatal("normal proxy of the non-reverse user is broken")
	}
}

func TestReversePortConflictAndLimit(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	listener1, err := stack.authorized.DialReverse(context.Background(), stack.ports[0])
	common.Must(err)
	defer listener1.Close()

	// the same port is already owned -> code 2, no matter the identity
	_, err = stack.authorized.DialReverse(context.Background(), stack.ports[0])
	expectBindCode(t, err, reverse.CodeInUse)
	_, err = stack.unauthorized.DialReverse(context.Background(), stack.ports[0])
	expectBindCode(t, err, reverse.CodeACL) // not in reverse.users at all

	// max-listeners=2 for this user: two live listeners are fine
	listener2, err := stack.authorized.DialReverse(context.Background(), stack.ports[1])
	common.Must(err)
	defer listener2.Close()

	// the third exceeds the quota -> code 4
	_, err = stack.authorized.DialReverse(context.Background(), stack.ports[2])
	expectBindCode(t, err, reverse.CodeLimit)
}

func TestReverseReconnect(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	port := stack.ports[0]
	listener, err := stack.authorized.DialReverse(context.Background(), port,
		reverse.WithTarget(util.EchoAddr),
		reverse.WithRetry(reverse.RetryPolicy{Initial: 200 * time.Millisecond, Max: time.Second}))
	common.Must(err)
	defer listener.Close()

	if !echoOnce(t, port, 5*time.Second) {
		t.Fatal("reverse forward does not work before the session kill")
	}
	if killed := stack.reverse.CloseAllSessions(); killed == 0 {
		t.Fatal("no reverse session to kill")
	}

	// the listener must rebind by itself
	deadline := time.Now().Add(15 * time.Second)
	for !echoOnce(t, port, 5*time.Second) {
		if time.Now().After(deadline) {
			t.Fatal("reverse listener failed to rebind after session loss")
		}
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case <-listener.Done():
		t.Fatal("listener gave up after session loss")
	default:
	}
}

func TestReverseAccept(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	port := stack.ports[0]
	listener, err := stack.authorized.DialReverse(context.Background(), port)
	common.Must(err)
	defer listener.Close()

	visitor, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	common.Must(err)
	defer visitor.Close()

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	resultChan := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		resultChan <- acceptResult{conn: conn, err: err}
	}()
	select {
	case result := <-resultChan:
		common.Must(result.err)
		if !util.CheckConn(visitor, result.conn) {
			t.Fatal("reverse accept connection check failed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for Accept")
	}
	if listener.Addr() == nil {
		t.Fatal("listener Addr is nil")
	}
}

func TestReverseNoGoroutineLeak(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	base := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		port := stack.ports[i]
		listener, err := stack.authorized.DialReverse(context.Background(), port, reverse.WithTarget(util.EchoAddr))
		common.Must(err)
		if !echoOnce(t, port, 5*time.Second) {
			listener.Close()
			t.Fatalf("iteration %d: echo failed", i)
		}
		listener.Close()
		time.Sleep(50 * time.Millisecond)
	}

	deadline := time.Now().Add(8 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if now := runtime.NumGoroutine(); now > base {
		t.Fatalf("goroutine leak: base=%d now=%d", base, now)
	}
}

// TestReverseListenerConnected pins the Connected() liveness surface used by
// upstream failover watchdogs: true while bound, false after a session loss
// (during the rebinding backoff), true again after a successful rebind, and
// false after Close.
func TestReverseListenerConnected(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	port := stack.ports[0]
	listener, err := stack.authorized.DialReverse(context.Background(), port,
		reverse.WithTarget(util.EchoAddr),
		reverse.WithRetry(reverse.RetryPolicy{Initial: 200 * time.Millisecond, Max: time.Second}))
	common.Must(err)
	defer listener.Close()

	if !listener.Connected() {
		t.Fatal("listener must report connected right after a successful bind")
	}

	if killed := stack.reverse.CloseAllSessions(); killed == 0 {
		t.Fatal("no reverse session to kill")
	}
	// Session death must reflect in Connected() promptly (markDead ->
	// maintainLoop teardown), well before the rebind completes.
	deadline := time.Now().Add(5 * time.Second)
	for listener.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("Connected() stayed true after the session was killed")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// After the listener rebinds by itself, Connected() must flip back.
	deadline = time.Now().Add(15 * time.Second)
	for !echoOnce(t, port, 5*time.Second) || !listener.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("reverse listener failed to rebind (or Connected stayed false) after session loss")
		}
		time.Sleep(200 * time.Millisecond)
	}

	listener.Close()
	if listener.Connected() {
		t.Fatal("Connected() must be false after Close")
	}
}
