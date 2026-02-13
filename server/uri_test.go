package server

import (
	"strings"
	"testing"
)

func TestURIOpenBlocklist(t *testing.T) {
	// Replace opener with a no-op so tests don't launch real applications.
	origOpener := opener
	var opened string
	opener = func(uri string) error {
		opened = uri
		return nil
	}
	t.Cleanup(func() { opener = origOpener })

	u := NewURI()

	tests := []struct {
		name    string
		uri     string
		wantErr string // substring of expected error, empty means no error
	}{
		// Allowed: bare hostnames
		{name: "bare hostname", uri: "google.com"},
		{name: "bare hostname with path", uri: "example.com/page"},

		// Allowed: http and https
		{name: "http", uri: "http://example.com"},
		{name: "https", uri: "https://example.com"},
		{name: "https with path", uri: "https://example.com/foo/bar?q=1"},
		{name: "HTTP uppercase", uri: "HTTP://example.com"},

		// Blocked schemes
		{name: "file", uri: "file:///etc/passwd", wantErr: `"file" is not allowed`},
		{name: "FILE uppercase", uri: "FILE:///etc/passwd", wantErr: `"file" is not allowed`},
		{name: "data", uri: "data:text/html,<h1>hi</h1>", wantErr: `"data" is not allowed`},
		{name: "javascript", uri: "javascript:alert(1)", wantErr: `"javascript" is not allowed`},
		{name: "vbscript", uri: "vbscript:MsgBox", wantErr: `"vbscript" is not allowed`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opened = ""
			err := u.Open(tc.uri, nil)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				if opened != "" {
					t.Fatalf("opener should not have been called for blocked URI, but got %q", opened)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if opened != tc.uri {
					t.Fatalf("expected opener to receive %q, got %q", tc.uri, opened)
				}
			}
		})
	}
}
