package server

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestTunnelEndpointRoundTrip(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	key := []byte("0123456789abcdef0123456789abcdef")
	client := newTunnelEndpoint(clientConn, key)
	server := newTunnelEndpoint(serverConn, key)

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.writeFrame(tunnelFrame{Type: tunnelFrameData, StreamID: 7, Payload: []byte("hello")})
	}()

	frame, err := server.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if frame.Type != tunnelFrameData || frame.StreamID != 7 || string(frame.Payload) != "hello" {
		t.Fatalf("unexpected frame: %#v", frame)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
}

func TestTunnelAttachAndBrowserOpen(t *testing.T) {
	origOpener := opener
	opened := make(chan string, 1)
	opener = func(uri string) error {
		opened <- uri
		return nil
	}
	defer func() { opener = origOpener }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	tn := NewTunnel()
	key := []byte("0123456789abcdef0123456789abcdef")
	var resp TunnelOpenResponse
	if err := tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:" + strconv.Itoa(port), Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, MACKey: key, AttachTimeout: time.Second, IdleTimeout: time.Second}, &resp); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tn.closeSession(resp.SessionID)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go tn.attach(serverConn)
	attachConn := clientConn
	defer attachConn.Close()
	attachEP := newTunnelEndpoint(attachConn, key)
	if err := attachEP.writeFrame(tunnelFrame{Type: tunnelFrameAttach, Payload: []byte(resp.SessionID)}); err != nil {
		t.Fatalf("attach write: %v", err)
	}

	browserConn, err := net.Dial("tcp", resp.ListenAddrs[0])
	if err != nil {
		t.Fatalf("Dial browser listener: %v", err)
	}
	defer browserConn.Close()

	frame, err := attachEP.readFrame()
	if err != nil {
		t.Fatalf("read open frame: %v", err)
	}
	if frame.Type != tunnelFrameOpen {
		t.Fatalf("frame.Type = %d, want open", frame.Type)
	}
	if frame.StreamID == 0 {
		t.Fatal("expected non-zero stream id")
	}
	if string(frame.Payload) == "" {
		t.Fatal("expected tunnel open payload")
	}

	select {
	case got := <-opened:
		if got != resp.OpenURL {
			t.Fatalf("opened = %q, want %q", got, resp.OpenURL)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for opener")
	}
}

func TestTunnelOpenAllowsNonLoopbackBrowserURL(t *testing.T) {
	tn := NewTunnel()
	key := []byte("0123456789abcdef0123456789abcdef")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	var resp TunnelOpenResponse
	err = tn.Open(TunnelOpenRequest{
		URL: "https://login.example.com/oauth2/authorize",
		Targets: []TunnelTarget{{
			ListenHost: "127.0.0.1",
			ListenPort: port,
			DialAddr:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		}},
		MACKey:        key,
		AttachTimeout: time.Second,
		IdleTimeout:   time.Second,
	}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tn.closeSession(resp.SessionID)
}
