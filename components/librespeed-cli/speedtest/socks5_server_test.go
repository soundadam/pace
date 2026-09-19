package speedtest

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
)

// socks5Request records what a client asked the proxy to connect to.
type socks5Request struct {
	AddressType byte // 0x01 IPv4, 0x03 domain name, 0x04 IPv6
	Host        string
	Port        uint16
	Methods     []byte // the authentication methods the client offered
}

// socks5Server is a deliberately small RFC 1928 server used to exercise the
// fork's --proxy path without touching the network beyond loopback.
type socks5Server struct {
	// MethodReply is the authentication method the server selects. 0x00 is
	// "no authentication"; 0xFF means "no acceptable methods"; 0x02 demands
	// username/password, which the fork never offers.
	MethodReply byte
	// ReplyCode is the CONNECT reply status. 0x00 succeeds, 0x05 is
	// "connection refused" and 0x04 is "host unreachable".
	ReplyCode byte
	// Target, when set, is the address the proxy really dials, regardless of
	// what the client requested. This is what makes socks5h name resolution
	// observable without DNS.
	Target string
	// HangAfterGreeting makes the server stop responding after the method
	// negotiation, so a dial blocks until its context is cancelled.
	HangAfterGreeting bool

	listener net.Listener
	mu       sync.Mutex
	requests []socks5Request
}

func startSOCKS5(t *testing.T, server *socks5Server) *socks5Server {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server.listener = listener
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(conn)
		}
	}()
	return server
}

func (s *socks5Server) Addr() string { return s.listener.Addr().String() }

func (s *socks5Server) Port() string {
	_, port, _ := net.SplitHostPort(s.Addr())
	return port
}

// Close stops the proxy so that subsequent dials are refused.
func (s *socks5Server) Close() { _ = s.listener.Close() }

func (s *socks5Server) Requests() []socks5Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]socks5Request(nil), s.requests...)
}

func (s *socks5Server) serve(conn net.Conn) {
	defer conn.Close()

	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if header[0] != 0x05 {
		return
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	method := s.MethodReply
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		return
	}
	if method != 0x00 {
		return
	}
	if s.HangAfterGreeting {
		// Hold the connection open without ever answering the CONNECT.
		_, _ = io.Copy(io.Discard, conn)
		return
	}

	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return
	}
	recorded := socks5Request{AddressType: request[3], Methods: methods}

	switch request[3] {
	case 0x01:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		recorded.Host = net.IP(addr).String()
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		name := make([]byte, length[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return
		}
		recorded.Host = string(name)
	case 0x04:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		recorded.Host = net.IP(addr).String()
	default:
		return
	}

	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return
	}
	recorded.Port = binary.BigEndian.Uint16(port)

	s.mu.Lock()
	s.requests = append(s.requests, recorded)
	s.mu.Unlock()

	if s.ReplyCode != 0x00 {
		_, _ = conn.Write([]byte{0x05, s.ReplyCode, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	target := s.Target
	if target == "" {
		target = net.JoinHostPort(recorded.Host, strconv.Itoa(int(recorded.Port)))
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	var relay sync.WaitGroup
	relay.Add(2)
	go func() {
		defer relay.Done()
		_, _ = io.Copy(upstream, conn)
		if tcp, ok := upstream.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	go func() {
		defer relay.Done()
		_, _ = io.Copy(conn, upstream)
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	relay.Wait()
}

// freeLoopbackPort returns a loopback port with nothing listening on it.
func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("close: %v", err)
	}
	return port
}
