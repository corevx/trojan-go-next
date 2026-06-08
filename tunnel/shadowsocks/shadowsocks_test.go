package shadowsocks

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

func TestShadowsocks(t *testing.T) {
	p, err := strconv.ParseInt(util.HTTPPort, 10, 32)
	common.Must(err)

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

	cfg := &Config{
		RemoteHost: "127.0.0.1",
		RemotePort: int(p),
		Shadowsocks: ShadowsocksConfig{
			Enabled:  true,
			Method:   "AES-128-GCM",
			Password: "password",
		},
	}
	ctx = config.WithConfig(ctx, Name, cfg)

	c, err := NewClient(ctx, tcpClient)
	common.Must(err)
	s, err := NewServer(ctx, tcpServer)
	common.Must(err)

	t.Cleanup(func() {
		c.Close()
		s.Close()
	})

	wg := sync.WaitGroup{}
	wg.Add(2)
	errChan := make(chan error, 2)
	var conn1, conn2 net.Conn
	go func() {
		defer wg.Done()
		var err error
		conn1, err = c.DialConn(nil, nil)
		if err != nil {
			errChan <- err
			return
		}
		conn1.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn1.Write(util.GeneratePayload(1024)); err != nil {
			errChan <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		conn2, err = s.AcceptConn(nil)
		if err != nil {
			errChan <- err
			return
		}
		conn2.SetDeadline(time.Now().Add(5 * time.Second))
		buf := [1024]byte{}
		if _, err := io.ReadFull(conn2, buf[:]); err != nil {
			errChan <- err
		}
	}()
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			t.Fatal(err)
		}
	}
	conn1.SetDeadline(time.Now().Add(5 * time.Second))
	conn2.SetDeadline(time.Now().Add(5 * time.Second))
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}

	acceptErr := make(chan error, 1)
	go func() {
		_, err := s.AcceptConn(nil)
		if err == nil {
			err = fmt.Errorf("expected invalid shadowsocks connection")
		}
		acceptErr <- err
	}()

	// test redirection
	conn3, err := tcpClient.DialConn(nil, nil)
	common.Must(err)
	conn3.SetDeadline(time.Now().Add(5 * time.Second))
	n, err := conn3.Write(util.GeneratePayload(1024))
	common.Must(err)
	fmt.Println("write:", n)
	buf := [1024]byte{}
	n, err = conn3.Read(buf[:])
	common.Must(err)
	fmt.Println("read:", n)
	if !strings.Contains(string(buf[:n]), "Bad Request") {
		t.Fail()
	}
	select {
	case err := <-acceptErr:
		if err == nil {
			t.Fatal("expected invalid shadowsocks connection")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for invalid shadowsocks connection")
	}
	conn1.Close()
	conn3.Close()
}
