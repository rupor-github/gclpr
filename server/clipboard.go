package server

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/atotto/clipboard"
)

// MaxClipboardSize is the maximum allowed clipboard payload size (1 MiB).
const MaxClipboardSize = 1 << 20

// Clipboard is used to rpc clipboard content.
type Clipboard struct {
	leOP     string
	writeAll func(string) error
}

// NewClipboard initializes Clipboard structure.
func NewClipboard(le string) *Clipboard {
	return &Clipboard{leOP: le, writeAll: clipboard.WriteAll}
}

// Copy is implementation of rpc "copy" command.
func (c *Clipboard) Copy(text string, _ *struct{}) error {
	log.Printf("Copy request received len: %d\n", len(text))
	if len(text) > MaxClipboardSize {
		return fmt.Errorf("clipboard payload size %d exceeds maximum %d", len(text), MaxClipboardSize)
	}
	// Far may include NUL terminators, but embedded NULs can panic the Windows clipboard backend.
	text = strings.TrimRight(text, "\x00")
	if strings.ContainsRune(text, '\x00') {
		return errors.New("clipboard payload contains embedded NUL")
	}
	return c.writeAll(ConvertLE(text, c.leOP))
}

// Paste is implementation of rpc "paste" command.
func (c *Clipboard) Paste(_ struct{}, resp *string) error {
	t, err := clipboard.ReadAll()
	log.Printf("Paste request received len: %d, error: '%+v'\n", len(t), err)
	*resp = t
	return err
}
