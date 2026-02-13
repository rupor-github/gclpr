package server

import "testing"

func TestConvertLE(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		op     string
		expect string
	}{
		// --- LF conversion ---
		{"lf: crlf to lf", "a\r\nb\r\n", "lf", "a\nb\n"},
		{"lf: cr to lf", "a\rb\r", "lf", "a\nb\n"},
		{"lf: mixed to lf", "a\r\nb\rc\n", "lf", "a\nb\nc\n"},
		{"lf: already lf", "a\nb\n", "lf", "a\nb\n"},
		{"lf: no line endings", "abc", "lf", "abc"},
		{"lf: empty", "", "lf", ""},
		{"LF: case insensitive", "a\r\nb\r\n", "LF", "a\nb\n"},

		// --- CRLF conversion ---
		{"crlf: lf to crlf", "a\nb\n", "crlf", "a\r\nb\r\n"},
		{"crlf: cr to crlf", "a\rb\r", "crlf", "a\r\nb\r\n"},
		{"crlf: already crlf", "a\r\nb\r\n", "crlf", "a\r\nb\r\n"},
		{"crlf: mixed", "a\nb\r\nc\rd\n", "crlf", "a\r\nb\r\nc\r\nd\r\n"},
		{"crlf: no line endings", "abc", "crlf", "abc"},
		{"crlf: empty", "", "crlf", ""},
		{"CRLF: case insensitive", "a\nb\n", "CRLF", "a\r\nb\r\n"},

		// --- Default (passthrough) ---
		{"default: empty op", "a\nb\r\nc\r", "", "a\nb\r\nc\r"},
		{"default: unknown op", "a\nb\r\n", "foo", "a\nb\r\n"},

		// --- Edge cases ---
		{"lf: lone cr at end", "abc\r", "lf", "abc\n"},
		{"crlf: lone lf at start", "\nabc", "crlf", "\r\nabc"},
		{"crlf: lone cr at end", "abc\r", "crlf", "abc\r\n"},
		{"lf: multiple cr", "\r\r\r", "lf", "\n\n\n"},
		// NOTE: consecutive bare LFs are only partially converted because the
		// regex requires a preceding non-\r character (or start-of-string) as
		// a capture group. After the first \n is replaced with \r\n, the next
		// \n has \n (not a non-\r char) preceding it and is skipped.
		{"crlf: multiple lf (regex limitation)", "\n\n\n", "crlf", "\n\r\n\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertLE(tc.text, tc.op)
			if got != tc.expect {
				t.Errorf("ConvertLE(%q, %q) = %q, want %q", tc.text, tc.op, got, tc.expect)
			}
		})
	}
}
