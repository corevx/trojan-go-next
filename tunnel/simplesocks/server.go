package simplesocks

import (
	"context"
	"fmt"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/trojan"
)

// Server is a simplesocks server
type Server struct {
	underlay   tunnel.Server
	connChan   chan tunnel.Conn
	packetChan chan tunnel.PacketConn
	ctx        context.Context
	cancel     context.CancelFunc
}

func (s *Server) Close() error {
	s.cancel()
	return s.underlay.Close()
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.underlay.AcceptConn(&Tunnel{})
		if err != nil {
			log.Error(common.NewError("simplesocks failed to accept connection from underlying tunnel").Base(err))
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			continue
		}
		// the header read must not stall the accept loop: a stream which never
		// sends its simplesocks header would otherwise block all subsequent ones
		go s.handleStream(conn)
	}
}

func (s *Server) handleStream(conn tunnel.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Error(common.NewError("panic in simplesocks handleStream goroutine").Base(fmt.Errorf("%v", r)))
			conn.Close()
		}
	}()
	metadata := new(tunnel.Metadata)
	if err := metadata.ReadFrom(conn); err != nil {
		log.Error(common.NewError("simplesocks server faield to read header").Base(err))
		conn.Close()
		return
	}
	switch metadata.Command {
	case Connect:
		select {
		case s.connChan <- &Conn{
			metadata: metadata,
			Conn:     conn,
		}:
		case <-s.ctx.Done():
			conn.Close()
		}
	case Associate:
		select {
		case s.packetChan <- &PacketConn{
			PacketConn: trojan.PacketConn{
				Conn: conn,
			},
		}:
		case <-s.ctx.Done():
			conn.Close()
		}
	default:
		log.Error(common.NewError(fmt.Sprintf("simplesocks unknown command %d", metadata.Command)))
		conn.Close()
	}
}

func (s *Server) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) {
	select {
	case conn := <-s.connChan:
		return conn, nil
	case <-s.ctx.Done():
		return nil, common.NewError("simplesocks server closed")
	}
}

func (s *Server) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	select {
	case packetConn := <-s.packetChan:
		return packetConn, nil
	case <-s.ctx.Done():
		return nil, common.NewError("simplesocks server closed")
	}
}

func NewServer(ctx context.Context, underlay tunnel.Server) (*Server, error) {
	ctx, cancel := context.WithCancel(ctx)
	server := &Server{
		underlay:   underlay,
		ctx:        ctx,
		connChan:   make(chan tunnel.Conn, 32),
		packetChan: make(chan tunnel.PacketConn, 32),
		cancel:     cancel,
	}
	go server.acceptLoop()
	log.Debug("simplesocks server created")
	return server, nil
}
