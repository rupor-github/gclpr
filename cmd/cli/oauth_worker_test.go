package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rupor-github/gclpr/server"
)

func TestRunOAuthWorkerReadsHandshakeAndReportsOK(t *testing.T) {
	origStatusAddr := aWorkerStatusAddr
	origTimeout := aConnectTimeout
	origIOTimeout := aIOTimeout
	origStart := oauthWorkerStartTunnelClient
	t.Cleanup(func() {
		aWorkerStatusAddr = origStatusAddr
		aConnectTimeout = origTimeout
		aIOTimeout = origIOTimeout
		oauthWorkerStartTunnelClient = origStart
	})

	aConnectTimeout = time.Second
	aIOTimeout = time.Second

	type gotCall struct {
		resp   server.TunnelOpenResponse
		macKey []byte
	}
	gotCh := make(chan gotCall, 1)
	oauthWorkerStartTunnelClient = func(resp server.TunnelOpenResponse, macKey []byte, timeout time.Duration, onAttached func() error) error {
		gotCh <- gotCall{resp: resp, macKey: append([]byte(nil), macKey...)}
		return onAttached()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	aWorkerStatusAddr = ln.Addr().String()

	workerErrCh := make(chan error, 1)
	go func() {
		workerErrCh <- runOAuthWorker()
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(oauthWorkerHandshake{SessionID: "session-123", MACKey: "3031323334353637383961626364656630313233343536373839616263646566"}); err != nil {
		t.Fatalf("encode handshake: %v", err)
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read worker status: %v", err)
	}
	if strings.TrimSpace(line) != "OK" {
		t.Fatalf("worker status = %q, want OK", strings.TrimSpace(line))
	}

	select {
	case got := <-gotCh:
		if got.resp.SessionID != "session-123" {
			t.Fatalf("session id = %q, want %q", got.resp.SessionID, "session-123")
		}
		if string(got.macKey) != "0123456789abcdef0123456789abcdef" {
			t.Fatalf("mac key = %q", string(got.macKey))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker tunnel startup")
	}

	if err := <-workerErrCh; err != nil {
		t.Fatalf("runOAuthWorker err = %v", err)
	}
}

func TestRunOAuthWorkerRejectsInvalidHandshake(t *testing.T) {
	origStatusAddr := aWorkerStatusAddr
	origTimeout := aConnectTimeout
	origStart := oauthWorkerStartTunnelClient
	t.Cleanup(func() {
		aWorkerStatusAddr = origStatusAddr
		aConnectTimeout = origTimeout
		oauthWorkerStartTunnelClient = origStart
	})

	aConnectTimeout = time.Second
	oauthWorkerStartTunnelClient = func(resp server.TunnelOpenResponse, macKey []byte, timeout time.Duration, onAttached func() error) error {
		t.Fatal("startTunnelClient should not be called for invalid handshake")
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	aWorkerStatusAddr = ln.Addr().String()

	workerErrCh := make(chan error, 1)
	go func() {
		workerErrCh <- runOAuthWorker()
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "not-json\n"); err != nil {
		t.Fatalf("write invalid handshake: %v", err)
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read worker status: %v", err)
	}
	if got := strings.TrimSpace(line); !strings.HasPrefix(got, "ERR oauth worker failed to read startup payload:") {
		t.Fatalf("worker status = %q", got)
	}

	if err := <-workerErrCh; err == nil || !strings.Contains(err.Error(), "failed to read startup payload") {
		t.Fatalf("runOAuthWorker err = %v", err)
	}
}
