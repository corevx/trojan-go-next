package simplesocks

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

func TestSimpleSocks(t *testing.T) {
	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	c, err := NewClient(ctx, tcpClient)
	common.Must(err)
	s, err := NewServer(ctx, tcpServer)
	common.Must(err)

	conn1, err := c.DialConn(&tunnel.Address{
		DomainName:  "www.baidu.com",
		AddressType: tunnel.DomainName,
		Port:        443,
	}, nil)
	common.Must(err)
	defer conn1.Close()
	conn1.Write(util.GeneratePayload(1024))
	conn2, err := s.AcceptConn(nil)
	common.Must(err)
	defer conn2.Close()
	buf := [1024]byte{}
	common.Must2(conn2.Read(buf[:]))
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}

	packet1, err := c.DialPacket(nil)
	common.Must(err)
	packet1.WriteWithMetadata([]byte("12345678"), &tunnel.Metadata{
		Address: &tunnel.Address{
			DomainName:  "test.com",
			AddressType: tunnel.DomainName,
			Port:        443,
		},
	})
	defer packet1.Close()
	packet2, err := s.AcceptPacket(nil)
	common.Must(err)
	defer packet2.Close()
	_, m, err := packet2.ReadWithMetadata(buf[:])
	common.Must(err)
	fmt.Println(m)

	if !util.CheckPacketOverConn(packet1, packet2) {
		t.Fail()
	}
	s.Close()
	c.Close()
}

// TestSimpleSocksSilentStreamNotBlockingAccept ensures a stream which is accepted
// but never sends its simplesocks header cannot stall the accept loop: the next
// stream must still complete the handshake, and the silent stream must be served
// once it eventually speaks.
func TestSimpleSocksSilentStreamNotBlockingAccept(t *testing.T) {
	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	c, err := NewClient(ctx, tcpClient)
	common.Must(err)
	s, err := NewServer(ctx, tcpServer)
	common.Must(err)
	defer s.Close()
	defer c.Close()

	// stream 1: accepted by the server, but never sends the simplesocks header
	silent, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	common.Must(err)
	defer silent.Close()
	// make sure the server has accepted the silent stream first, so it sits at
	// the head of the accept queue (otherwise the regression would not trigger)
	time.Sleep(500 * time.Millisecond)

	// stream 2: a normal stream must complete the handshake and be accepted
	conn1, err := c.DialConn(&tunnel.Address{
		DomainName:  "www.baidu.com",
		AddressType: tunnel.DomainName,
		Port:        443,
	}, nil)
	common.Must(err)
	defer conn1.Close()
	common.Must2(conn1.Write(util.GeneratePayload(1024)))

	acceptChan := make(chan tunnel.Conn, 2)
	go func() {
		defer close(acceptChan)
		for {
			conn, err := s.AcceptConn(nil)
			if err != nil {
				return
			}
			acceptChan <- conn
		}
	}()

	select {
	case conn2, ok := <-acceptChan:
		if !ok {
			t.Fatal("accept pump exited before accepting the normal stream")
		}
		defer conn2.Close()
		buf := [1024]byte{}
		common.Must2(conn2.Read(buf[:]))
		if !util.CheckConn(conn1, conn2) {
			t.Fatal("connection check failed for the normal stream")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("accept loop stalled by silent stream: AcceptConn timed out")
	}

	// the silent stream is still pending: once it finally sends its header it
	// must be served as well, proving its handler was parked, not dropped
	common.Must((&tunnel.Metadata{
		Command: Connect,
		Address: &tunnel.Address{
			DomainName:  "late.example.com",
			AddressType: tunnel.DomainName,
			Port:        443,
		},
	}).WriteTo(silent))

	select {
	case conn3, ok := <-acceptChan:
		if !ok {
			t.Fatal("accept pump exited before accepting the late stream")
		}
		defer conn3.Close()
		if conn3.Metadata().DomainName != "late.example.com" {
			t.Fatalf("unexpected metadata on late stream: %s", conn3.Metadata())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("late header on silent stream was never accepted")
	}
}
