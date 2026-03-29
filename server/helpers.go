package server

import (
	"strings"
)

// ConvertLE is used to normalize line endings when exchanging clipboard content.
func ConvertLE(text, op string) string {
	switch {
	case strings.EqualFold("lf", op):
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		return text
	case strings.EqualFold("crlf", op):
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		return strings.ReplaceAll(text, "\n", "\r\n")
	default:
		return text
	}
}
