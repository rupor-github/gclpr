package server

import (
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/skratchdot/open-golang/open"
)

// blockedSchemes lists URI schemes that should not be passed to the OS opener.
var blockedSchemes = map[string]bool{
	"file":       true,
	"data":       true,
	"javascript": true,
	"vbscript":   true,
}

// opener is the function used to open URIs. It defaults to open.Run and can be
// overridden in tests to avoid launching real applications.
var opener = open.Run

// URI is used to rpc open command.
type URI struct {
	// placeholder
}

// NewURI initializes URI structure.
func NewURI() *URI {
	return &URI{}
}

// Open is implementation of "lemonade" rpc "open" command.
func (u *URI) Open(uri string, _ *struct{}) error {
	log.Printf("URI Open received: '%s'", uri)

	parsed, err := ParseOpenURI(uri)
	if err != nil {
		return err
	}

	return opener(normalizeOpenURI(parsed, uri))
}

// ParseOpenURI validates that the URI can be safely passed to the OS opener.
func ParseOpenURI(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URI: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if blockedSchemes[scheme] {
		return nil, fmt.Errorf("URI scheme %q is not allowed", scheme)
	}

	return parsed, nil
}

func normalizeOpenURI(parsed *url.URL, raw string) string {
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(raw, "//") {
		return raw
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return raw
	}
	if vol := filepath.VolumeName(raw); vol != "" {
		return raw
	}
	return "https://" + raw
}
