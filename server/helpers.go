package server

import (
	"regexp"
	"strings"
)

var (
	reCRNotCRLF = regexp.MustCompile(`\r(.)|\r$`)
	reLFNotCRLF = regexp.MustCompile(`([^\r])\n|^\n`)
)

// ConvertLE is used to normaliza line endings when exchanging clipboard content.
func ConvertLE(text, op string) string {
	switch {
	case strings.EqualFold("lf", op):
		text = strings.ReplaceAll(text, "\r\n", "\n")
		return strings.ReplaceAll(text, "\r", "\n")
	case strings.EqualFold("crlf", op):
		text = reCRNotCRLF.ReplaceAllString(text, "\r\n$1")
		text = reLFNotCRLF.ReplaceAllString(text, "$1\r\n")
		return text
	default:
		return text
	}
}
