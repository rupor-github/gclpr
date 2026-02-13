package util

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MaxFrameSize is the maximum allowed frame payload size (16 MiB).
// This prevents memory exhaustion from malformed or malicious length prefixes.
const MaxFrameSize = 16 << 20

// WriteFrame writes data as a length-prefixed frame: 4 bytes big-endian length
// followed by the payload. The length field covers only the payload bytes.
func WriteFrame(w io.Writer, data []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("frame: writing length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("frame: writing payload: %w", err)
	}
	return nil
}

// ReadFrame reads a length-prefixed frame from r: 4 bytes big-endian length
// followed by that many bytes of payload. Returns the payload.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrameSize {
		return nil, fmt.Errorf("frame: payload size %d exceeds maximum %d", n, MaxFrameSize)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("frame: reading payload: %w", err)
	}
	return buf, nil
}
