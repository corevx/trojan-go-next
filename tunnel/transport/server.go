package transport

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

// Server is a server of transport layer
type Server struct {
	tcpListener net.Listener
	cmd         *exec.Cmd
	connChan    chan tunnel.Conn
	wsChan      chan tunnel.Conn
	httpLock    sync.RWMutex
	nextHTTP    bool
	ctx         context.Context
	cancel      context.CancelFunc
}

func (s *Server) Close() error {
	s.cancel()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	return s.tcpListener.Close()
}

// Accept retry backoff: starts small (fd exhaustion may clear within milliseconds)
// and doubles up to acceptBackoffMax, at which point failures are logged at most
// once per second instead of spinning.
const (
	acceptBackoffStart = 5 * time.Millisecond
	acceptBackoffMax   = time.Second
)

// isListenerClosed reports whether an Accept error means the listener itself is
// gone (i.e. Server.Close or listener failure), so acceptLoop must exit instead
// of retrying. Everything else (EMFILE/ENFILE/ECONNABORTED/EPROTO/...) is
// treated as transient and retried. errors.Is sees through common.Error
// chains via Unwrap, so wrapped accept errors are matched exactly.
func isListenerClosed(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed)
}

func (s *Server) acceptLoop() {
	backoff := acceptBackoffStart
	for {
		tcpConn, err := s.tcpListener.Accept()
		if err != nil {
			if isListenerClosed(err) {
				return
			}
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			// transient error: retry with exponential backoff so the server
			// recovers automatically once the condition (e.g. fd pressure) clears
			log.Error(common.NewError("transport accept error, retrying in " + backoff.String()).Base(err))
			select {
			case <-time.After(backoff):
			case <-s.ctx.Done():
				return
			}
			backoff *= 2
			if backoff > acceptBackoffMax {
				backoff = acceptBackoffMax
			}
			continue
		}
		backoff = acceptBackoffStart
		go s.handleConn(tcpConn)
	}
}

func (s *Server) handleConn(tcpConn net.Conn) {
	log.Info("tcp connection from", tcpConn.RemoteAddr())
	s.httpLock.RLock()
	if s.nextHTTP { // plaintext mode enabled
		s.httpLock.RUnlock()
		// we use real http header parser to mimic a real http server
		rewindConn := common.NewRewindConn(tcpConn)
		rewindConn.SetBufferSize(512)
		defer rewindConn.StopBuffering()

		r := bufio.NewReader(rewindConn)
		httpReq, err := http.ReadRequest(r)
		rewindConn.Rewind()
		rewindConn.StopBuffering()
		if err != nil {
			// this is not a http request, pass it to trojan protocol layer for further inspection
			s.connChan <- &Conn{
				Conn: rewindConn,
			}
		} else {
			// this is a http request, pass it to websocket protocol layer
			log.Debug("plaintext http request: ", httpReq)
			s.wsChan <- &Conn{
				Conn: rewindConn,
			}
		}
	} else {
		s.httpLock.RUnlock()
		s.connChan <- &Conn{
			Conn: tcpConn,
		}
	}
}

func (s *Server) AcceptConn(overlay tunnel.Tunnel) (tunnel.Conn, error) {
	// TODO fix import cycle
	if overlay != nil && (overlay.Name() == "WEBSOCKET" || overlay.Name() == "HTTP") {
		s.httpLock.Lock()
		s.nextHTTP = true
		s.httpLock.Unlock()
		select {
		case conn := <-s.wsChan:
			return conn, nil
		case <-s.ctx.Done():
			return nil, common.NewError("transport server closed")
		}
	}
	select {
	case conn := <-s.connChan:
		return conn, nil
	case <-s.ctx.Done():
		return nil, common.NewError("transport server closed")
	}
}

func (s *Server) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

// NewServer creates a transport layer server
func NewServer(ctx context.Context, _ tunnel.Server) (*Server, error) {
	cfg := config.FromContext(ctx, Name).(*Config)
	listenAddress := tunnel.NewAddressFromHostPort("tcp", cfg.LocalHost, cfg.LocalPort)

	var cmd *exec.Cmd
	if cfg.TransportPlugin.Enabled {
		log.Warn("transport server will use plugin and work in plain text mode")
		switch cfg.TransportPlugin.Type {
		case "shadowsocks":
			trojanHost := "127.0.0.1"
			trojanPort := common.PickPort("tcp", trojanHost)
			cfg.TransportPlugin.Env = append(
				cfg.TransportPlugin.Env,
				"SS_REMOTE_HOST="+cfg.LocalHost,
				"SS_REMOTE_PORT="+strconv.FormatInt(int64(cfg.LocalPort), 10),
				"SS_LOCAL_HOST="+trojanHost,
				"SS_LOCAL_PORT="+strconv.FormatInt(int64(trojanPort), 10),
				"SS_PLUGIN_OPTIONS="+cfg.TransportPlugin.Option,
			)

			cfg.LocalHost = trojanHost
			cfg.LocalPort = trojanPort
			listenAddress = tunnel.NewAddressFromHostPort("tcp", cfg.LocalHost, cfg.LocalPort)
			log.Debug("new listen address", listenAddress)
			log.Debug("plugin env", cfg.TransportPlugin.Env)

			cmd = exec.Command(cfg.TransportPlugin.Command, cfg.TransportPlugin.Arg...)
			cmd.Env = append(cmd.Env, cfg.TransportPlugin.Env...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
			cmd.Start()
		case "other":
			cmd = exec.Command(cfg.TransportPlugin.Command, cfg.TransportPlugin.Arg...)
			cmd.Env = append(cmd.Env, cfg.TransportPlugin.Env...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
			cmd.Start()
		case "plaintext":
			// do nothing
		default:
			return nil, common.NewError("invalid plugin type: " + cfg.TransportPlugin.Type)
		}
	}
	tcpListener, err := net.Listen("tcp", listenAddress.String())
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	server := &Server{
		tcpListener: tcpListener,
		cmd:         cmd,
		ctx:         ctx,
		cancel:      cancel,
		connChan:    make(chan tunnel.Conn, 32),
		wsChan:      make(chan tunnel.Conn, 32),
	}
	go server.acceptLoop()
	return server, nil
}
