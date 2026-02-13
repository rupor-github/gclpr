package server

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/skratchdot/open-golang/open"
)

// allowedSchemes lists URI schemes that are safe to open.
var allowedSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

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

	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid URI: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if !allowedSchemes[scheme] {
		return fmt.Errorf("URI scheme %q is not allowed", scheme)
	}

	return open.Run(uri)
}
