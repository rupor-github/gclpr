package server

import (
	"errors"
	"net"
	"net/rpc"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClipboardCopy(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		le      string
		want    string
		wantErr string
	}{
		{name: "plain text", text: "hello", want: "hello"},
		{name: "empty", text: "", want: ""},
		{name: "trailing NUL", text: "hello\x00", want: "hello"},
		{name: "trailing NULs", text: "hello\x00\x00", want: "hello"},
		{name: "only NULs", text: "\x00\x00", want: ""},
		{name: "embedded NUL", text: "hello\x00world", wantErr: "clipboard payload contains embedded NUL"},
		{name: "leading NUL", text: "\x00hello", wantErr: "clipboard payload contains embedded NUL"},
		{name: "embedded and trailing NULs", text: "a\x00b\x00\x00", wantErr: "clipboard payload contains embedded NUL"},
		{name: "line ending conversion", text: "a\r\nb\x00\x00", le: "lf", want: "a\nb"},
		{name: "maximum size", text: strings.Repeat("a", MaxClipboardSize), want: strings.Repeat("a", MaxClipboardSize)},
		{name: "oversized", text: strings.Repeat("a", MaxClipboardSize+1), wantErr: "exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClipboard(tt.le)
			calls := 0
			c.writeAll = func(text string) error {
				calls++
				if text != tt.want {
					t.Errorf("clipboard writer received %q, want %q", text, tt.want)
				}
				return nil
			}

			err := c.Copy(tt.text, &struct{}{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Copy error = %v, want %q", err, tt.wantErr)
				}
				if calls != 0 {
					t.Errorf("clipboard writer called %d times for rejected payload", calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Copy: %v", err)
			}
			if calls != 1 {
				t.Errorf("clipboard writer called %d times, want 1", calls)
			}
		})
	}
}

func TestClipboardCopyWriteError(t *testing.T) {
	wantErr := errors.New("clipboard unavailable")
	c := NewClipboard("")
	c.writeAll = func(string) error { return wantErr }
	if err := c.Copy("hello\x00", &struct{}{}); !errors.Is(err, wantErr) {
		t.Errorf("Copy error = %v, want %v", err, wantErr)
	}
}

func TestClipboardCopyRPCRejectsEmbeddedNUL(t *testing.T) {
	c := NewClipboard("")
	var written string
	c.writeAll = func(text string) error {
		written = text
		return nil
	}
	srv := rpc.NewServer()
	if err := srv.Register(c); err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	deadline := time.Now().Add(5 * time.Second)
	if err := serverConn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := clientConn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() { srv.ServeConn(serverConn) })
	client := rpc.NewClient(clientConn)
	defer func() {
		client.Close()
		wg.Wait()
	}()

	err := client.Call("Clipboard.Copy", "a\x00b\x00", &struct{}{})
	var rpcErr rpc.ServerError
	if !errors.As(err, &rpcErr) || string(rpcErr) != "clipboard payload contains embedded NUL" {
		t.Fatalf("Copy error = %v, want embedded NUL RPC error", err)
	}
	// A rejected request must leave the server and connection usable.
	if err := client.Call("Clipboard.Copy", "hello\x00\x00", &struct{}{}); err != nil {
		t.Fatalf("Copy after rejected request: %v", err)
	}
	if written != "hello" {
		t.Errorf("clipboard writer received %q, want %q", written, "hello")
	}
}
