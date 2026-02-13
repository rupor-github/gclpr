package main

import (
	"io"
	"testing"
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
