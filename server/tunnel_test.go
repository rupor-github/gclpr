package server

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testTunnelMACKey = []byte("0123456789abcdef0123456789abcdef")

func TestParseTunnelURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "localhost no port", raw: "http://localhost"},
		{name: "ipv4 loopback", raw: "http://127.0.0.1:8080/path"},
		{name: "ipv6 loopback", raw: "https://[::1]/"},
		{name: "non loopback", raw: "http://example.com", wantErr: "loopback"},
		{name: "bad scheme", raw: "ftp://localhost", wantErr: "http or https"},
		{name: "relative", raw: "/foo", wantErr: "absolute URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseTunnelURL(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.String() != tc.raw {
				t.Fatalf("parsed = %q, want %q", parsed.String(), tc.raw)
			}
		})
	}
}

func TestTunnelEffectivePort(t *testing.T) {
	tests := []struct {
		url  string
		want int
	}{
		{url: "http://localhost", want: 80},
		{url: "https://localhost", want: 443},
		{url: "http://localhost:3000", want: 3000},
	}

	for _, tc := range tests {
		parsed, err := ParseTunnelURL(tc.url)
		if err != nil {
			t.Fatalf("ParseTunnelURL(%q): %v", tc.url, err)
		}
		got, err := tunnelEffectivePort(parsed)
		if err != nil {
			t.Fatalf("tunnelEffectivePort(%q): %v", tc.url, err)
		}
		if got != tc.want {
			t.Fatalf("tunnelEffectivePort(%q) = %d, want %d", tc.url, got, tc.want)
		}
	}
}

func TestTunnelOpenReservesRequestedPort(t *testing.T) {
	tn := NewTunnel()
	t.Cleanup(func() {
		tn.mu.Lock()
		ids := make([]string, 0, len(tn.sessions))
		for id := range tn.sessions {
			ids = append(ids, id)
		}
		tn.mu.Unlock()
		for _, id := range ids {
			tn.closeSession(id)
		}
	})

	var resp TunnelOpenResponse
	err := tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:0", Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: 0, DialAddr: net.JoinHostPort("127.0.0.1", "0")}}, MACKey: testTunnelMACKey}, &resp)
	if err == nil {
		t.Fatal("expected invalid port error")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	err = tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:" + strconv.Itoa(port), Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, MACKey: testTunnelMACKey}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if resp.ListenPort != port {
		t.Fatalf("ListenPort = %d, want %d", resp.ListenPort, port)
	}
	if len(resp.ListenAddrs) != 1 {
		t.Fatalf("ListenAddrs len = %d, want 1", len(resp.ListenAddrs))
	}
	tn.closeSession(resp.SessionID)
}

func TestTunnelOpenFallsBackToRandomPortOnConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	tn := NewTunnel()
	t.Cleanup(func() {
		tn.mu.Lock()
		ids := make([]string, 0, len(tn.sessions))
		for id := range tn.sessions {
			ids = append(ids, id)
		}
		tn.mu.Unlock()
		for _, id := range ids {
			tn.closeSession(id)
		}
	})
	var resp TunnelOpenResponse
	err = tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:" + strconv.Itoa(port), Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, MACKey: testTunnelMACKey}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if resp.ListenPort == port {
		t.Fatalf("ListenPort = %d, want random port instead of conflicted %d", resp.ListenPort, port)
	}
	if got, wantPrefix := resp.OpenURL, "http://127.0.0.1:"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("OpenURL = %q, want prefix %q", got, wantPrefix)
	}
	parsed, err := url.Parse(resp.OpenURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", resp.OpenURL, err)
	}
	if gotPort := parsed.Port(); gotPort != strconv.Itoa(resp.ListenPort) {
		t.Fatalf("OpenURL port = %q, want %d", gotPort, resp.ListenPort)
	}
}

func TestTunnelOpenRewritesOAuthRedirectPortOnConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	tn := NewTunnel()
	t.Cleanup(func() {
		tn.mu.Lock()
		ids := make([]string, 0, len(tn.sessions))
		for id := range tn.sessions {
			ids = append(ids, id)
		}
		tn.mu.Unlock()
		for _, id := range ids {
			tn.closeSession(id)
		}
	})

	var resp TunnelOpenResponse
	err = tn.Open(TunnelOpenRequest{
		URL:     "https://login.example.com/auth?redirect_uri=" + url.QueryEscape("http://127.0.0.1:"+strconv.Itoa(port)+"/callback"),
		Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}},
		MACKey:  testTunnelMACKey,
	}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if resp.ListenPort == port {
		t.Fatalf("ListenPort = %d, want random port instead of conflicted %d", resp.ListenPort, port)
	}
	parsed, err := url.Parse(resp.OpenURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", resp.OpenURL, err)
	}
	redirectValue := parsed.Query().Get("redirect_uri")
	if redirectValue == "" {
		t.Fatal("redirect_uri missing from rewritten OpenURL")
	}
	redirectURL, err := url.Parse(redirectValue)
	if err != nil {
		t.Fatalf("url.Parse(redirect_uri): %v", err)
	}
	if gotPort := redirectURL.Port(); gotPort != strconv.Itoa(resp.ListenPort) {
		t.Fatalf("redirect_uri port = %q, want %d", gotPort, resp.ListenPort)
	}
}

func TestTunnelOpenAttachTimeoutClosesSession(t *testing.T) {
	tn := NewTunnel()
	tn.newSessionID = func() (string, error) { return "session", nil }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var resp TunnelOpenResponse
	err = tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:" + strconv.Itoa(port), Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, MACKey: testTunnelMACKey, AttachTimeout: 20 * time.Millisecond}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	tn.mu.Lock()
	_, ok := tn.sessions[resp.SessionID]
	tn.mu.Unlock()
	if ok {
		t.Fatal("session still present after attach timeout")
	}
}

func TestTunnelIdleTimerClosesSession(t *testing.T) {
	tn := NewTunnel()
	tn.newSessionID = func() (string, error) { return "session", nil }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var resp TunnelOpenResponse
	err = tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:" + strconv.Itoa(port), Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, MACKey: testTunnelMACKey, AttachTimeout: time.Second, IdleTimeout: 20 * time.Millisecond}, &resp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	tn.mu.Lock()
	_, ok := tn.sessions[resp.SessionID]
	tn.mu.Unlock()
	if ok {
		t.Fatal("session still present after idle timeout")
	}
}

func TestTunnelOpenSessionIDFailure(t *testing.T) {
	tn := NewTunnel()
	tn.newSessionID = func() (string, error) { return "", errors.New("boom") }
	var resp TunnelOpenResponse
	err := tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1", Targets: []TunnelTarget{{ListenHost: "127.0.0.1", ListenPort: 80, DialAddr: net.JoinHostPort("127.0.0.1", "80")}}, MACKey: testTunnelMACKey}, &resp)
	if err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTunnelOpenRequiresTargets(t *testing.T) {
	tn := NewTunnel()
	var resp TunnelOpenResponse
	err := tn.Open(TunnelOpenRequest{URL: "http://127.0.0.1:3000", MACKey: testTunnelMACKey}, &resp)
	if err == nil || !strings.Contains(err.Error(), "tunnel targets are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
