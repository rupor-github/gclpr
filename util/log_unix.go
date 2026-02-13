//go:build linux || darwin

package util

import (
	"io"
	"log"
)

// NewLogWriter redirects all log output depending on debug parameter.
// When true all output goes to stderr.
// When false - everything is discarded.
func NewLogWriter(title string, flags int, debug bool) {

	log.SetPrefix("[" + title + "] ")
	log.SetFlags(flags)

	if !debug {
		log.SetOutput(io.Discard)
	}
}
