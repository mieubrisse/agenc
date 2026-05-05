package server

import "testing"

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no ansi", "hello world", "hello world"},
		{"color code", "\x1b[31mred\x1b[0m", "red"},
		{"cursor move", "before\x1b[2J\x1b[Hafter", "beforeafter"},
		{"OSC sequence with BEL", "\x1b]0;title\x07rest", "rest"},
		{"OSC sequence with ST", "\x1b]8;;url\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"unicode preserved", "héllo 🐚", "héllo 🐚"},
		{"markdown preserved", "# Header\n\n**bold**", "# Header\n\n**bold**"},
		{"empty string", "", ""},
		{"only escape", "\x1b[0m", ""},
		{"escape inside text", "before\x1b[1;31mred bold\x1b[0mafter", "beforered boldafter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripANSI(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
