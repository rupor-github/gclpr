package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGetCommandAliased(t *testing.T) {
	tests := []struct {
		name    string
		arg0    string
		wantCmd command
	}{
		{"xdg-open", "/usr/bin/xdg-open", cmdOpen},
		{"xdg-open bare", "xdg-open", cmdOpen},
		{"pbpaste", "/usr/bin/pbpaste", cmdPaste},
		{"pbpaste bare", "pbpaste", cmdPaste},
		{"pbcopy", "/usr/bin/pbcopy", cmdCopy},
		{"pbcopy bare", "pbcopy", cmdCopy},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, aliased, err := getCommand([]string{tc.arg0, "dummy"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !aliased {
				t.Error("expected aliased=true")
			}
			if cmd != tc.wantCmd {
				t.Errorf("got cmd=%v, want %v", cmd, tc.wantCmd)
			}
		})
	}
}

func TestGetCommandExplicit(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCmd command
	}{
		{"open", []string{"gclpr", "open", "http://example.com"}, cmdOpen},
		{"copy", []string{"gclpr", "copy", "text"}, cmdCopy},
		{"paste", []string{"gclpr", "paste"}, cmdPaste},
		{"server", []string{"gclpr", "server"}, cmdServer},
		{"genkey", []string{"gclpr", "genkey"}, cmdGenKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, aliased, err := getCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if aliased {
				t.Error("expected aliased=false")
			}
			if cmd != tc.wantCmd {
				t.Errorf("got cmd=%v, want %v", cmd, tc.wantCmd)
			}
		})
	}
}

func TestGetCommandUnknown(t *testing.T) {
	// Suppress usage output during test
	cli.SetOutput(io.Discard)

	_, _, err := getCommand([]string{"gclpr", "notacmd"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestGetCommandRemovesFromArgs(t *testing.T) {
	// When a command is found in args[1:], it should be removed from the slice
	args := []string{"gclpr", "copy", "--port", "1234"}
	cmd, aliased, err := getCommand(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aliased {
		t.Error("expected aliased=false")
	}
	if cmd != cmdCopy {
		t.Errorf("got cmd=%v, want cmdCopy", cmd)
	}
	// After getCommand, args should have "copy" removed and trailing empty string
	if args[1] != "--port" {
		t.Errorf("args[1] = %q, want %q", args[1], "--port")
	}
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		cmd  command
		want string
	}{
		{cmdOpen, "open url in server's default browser"},
		{cmdCopy, "send text to server clipboard"},
		{cmdPaste, "output server clipboard locally"},
		{cmdServer, "start server"},
		{cmdGenKey, "generate key pair for signing"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.cmd.String()
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	// Bad command
	bad := command(99)
	if s := bad.String(); s == "" {
		t.Error("expected non-empty string for bad command")
	}
}

func TestParseTunnelTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "localhost http", raw: "http://localhost:3000", wantErr: false},
		{name: "ipv4 loopback https", raw: "https://127.0.0.1:8443/path", wantErr: false},
		{name: "ipv6 loopback", raw: "http://[::1]:8080", wantErr: false},
		{name: "non absolute", raw: "/relative", wantErr: true},
		{name: "non loopback host", raw: "http://example.com", wantErr: true},
		{name: "unsupported scheme", raw: "ftp://localhost:21", wantErr: true},
		{name: "bare host without scheme", raw: "localhost:3000", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseTunnelTarget(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.String() != tc.raw {
				t.Fatalf("parsed.String() = %q, want %q", parsed.String(), tc.raw)
			}
		})
	}
}

func TestProcessCommandLineRejectsMixedTunnelModes(t *testing.T) {
	origTunnel, origOAuth := aTunnel, aOAuth
	t.Cleanup(func() {
		aTunnel = origTunnel
		aOAuth = origOAuth
	})

	_, err := processCommandLine([]string{"gclpr", "open", "-tunnel", "-oauth", "http://localhost:3000"})
	if err == nil {
		t.Fatal("expected error for mixed tunnel modes")
	}
}

func TestProcessCommandLineAliasedOpenDefaultsToOAuth(t *testing.T) {
	origOAuth := aOAuth
	t.Cleanup(func() { aOAuth = origOAuth })

	_, err := processCommandLine([]string{"xdg-open", "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !aOAuth {
		t.Fatal("expected aliased open to enable oauth mode by default")
	}
}

func TestAliasedOAuthFallbackDetection(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"xdg-open", "http://example.com"}

	if !reOpen.MatchString(os.Args[0]) {
		t.Fatal("expected xdg-open alias to match")
	}
}

func TestBuildTunnelTargetsFromOAuthURL(t *testing.T) {
	targets, err := buildTunnelTargetsFromOAuthURL("https://login.example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A33155")
	if err != nil {
		t.Fatalf("buildTunnelTargetsFromOAuthURL: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].ListenHost != "localhost" || targets[0].ListenPort != 33155 || targets[0].DialAddr != "127.0.0.1:33155" {
		t.Fatalf("unexpected target: %#v", targets[0])
	}
}

func TestEnvDebugEnabled(t *testing.T) {
	orig := os.Getenv("GCLPR_DEBUG")
	t.Cleanup(func() { _ = os.Setenv("GCLPR_DEBUG", orig) })
	if err := os.Setenv("GCLPR_DEBUG", "1"); err != nil {
		t.Fatal(err)
	}
	if !envDebugEnabled() {
		t.Fatal("expected env debug to be enabled")
	}
}

func TestGetCommandInternalOAuthWorker(t *testing.T) {
	cmd, aliased, err := getCommand([]string{"gclpr", "internal-oauth-worker"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aliased {
		t.Fatal("expected explicit command, not aliased")
	}
	if cmd != cmdOAuthWorker {
		t.Fatalf("cmd = %v, want %v", cmd, cmdOAuthWorker)
	}
}

func TestTunnelClientRemoteEOFCloseWrite(t *testing.T) {
	const macKey = "0123456789abcdef0123456789abcdef"

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	endpoint := newTunnelEndpoint(serverConn, []byte(macKey))
	client := newTunnelClient(endpoint)
	defer client.closeAll()

	localConn, remoteConn := net.Pipe()
	defer remoteConn.Close()
	stream := newTunnelLocalStream(7, localConn)
	client.streams[stream.id] = stream

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.readFrames()
	}()

	peer := &tunnelEndpoint{conn: clientConn, br: bufio.NewReader(clientConn), macKey: []byte(macKey)}
	if err := peer.writeFrame(tunnelFrame{Type: tunnelFrameEOF, StreamID: stream.id}); err != nil {
		t.Fatalf("writeFrame EOF: %v", err)
	}

	readDone := make(chan struct{})
	go func() {
		var buf [1]byte
		_, err := remoteConn.Read(buf[:])
		if err != io.EOF {
			t.Errorf("remote read err = %v, want EOF", err)
		}
		close(readDone)
	}()

	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for local CloseWrite propagation")
	}

	_ = endpoint.close()
	if err := <-errCh; err != nil && err != io.EOF && err != net.ErrClosed && !errors.Is(err, io.ErrClosedPipe) && !strings.Contains(err.Error(), "closed pipe") {
		t.Fatalf("readFrames err = %v", err)
	}
}

func TestTunnelClientFinishTreatsIntentionalCloseAsSuccess(t *testing.T) {
	const macKey = "0123456789abcdef0123456789abcdef"

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	endpoint := newTunnelEndpoint(serverConn, []byte(macKey))
	client := newTunnelClient(endpoint)

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.readFrames()
	}()

	client.finish()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("readFrames err = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for intentional close to finish")
	}
}
