package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/mieubrisse/stacktrace"
	"golang.org/x/term"
)

// errPromptCancelled is returned when the user presses ESC or Ctrl+C
// during an interactive prompt.
var errPromptCancelled = errors.New("prompt cancelled")

// readRawLine prompts the user for a single line of text using raw terminal
// mode. ESC and Ctrl+C cancel the prompt (returning errPromptCancelled).
// Falls back to simple bufio reading for non-terminal stdin.
func readRawLine(prompt string) (string, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd()) //nolint:gosec // G115: file descriptor fits in int
	if !term.IsTerminal(fd) {
		return readLineFromStdin()
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return readLineFromStdin()
	}
	defer term.Restore(fd, oldState) //nolint:errcheck // best-effort restore

	var runes []rune
	buf := make([]byte, 4) // max UTF-8 sequence length

	for {
		if _, err := os.Stdin.Read(buf[:1]); err != nil {
			fmt.Print("\r\n")
			return "", stacktrace.Propagate(err, "failed to read input")
		}

		result, done, keystrokeErr := handleRawKeystroke(buf[0], &runes, buf)
		if done {
			return result, keystrokeErr
		}
	}
}

// readLineFromStdin is the simple fallback for non-terminal stdin (e.g. piped input).
func readLineFromStdin() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read input")
	}
	return strings.TrimSpace(line), nil
}

// handleRawKeystroke processes a single byte read from stdin in raw terminal mode.
// It modifies the runes buffer in place and returns:
//   - done=true with the trimmed string when Enter is pressed
//   - done=true with errPromptCancelled when ESC or Ctrl+C is pressed
//   - done=false to continue reading
func handleRawKeystroke(b byte, runes *[]rune, buf []byte) (result string, done bool, err error) {
	switch {
	case b == 0x1B: // ESC byte
		if isStandaloneEsc() {
			fmt.Print("\r\n")
			return "", true, errPromptCancelled
		}

	case b == 0x03: // Ctrl+C
		fmt.Print("\r\n")
		return "", true, errPromptCancelled

	case b == 0x0D: // Enter (carriage return in raw mode)
		fmt.Print("\r\n")
		return strings.TrimSpace(string(*runes)), true, nil

	case b == 0x15: // Ctrl+U — clear entire line
		eraseLineDisplay(runes)

	case b == 0x7F || b == 0x08: // Backspace
		handleBackspace(runes)

	case b >= 0xC0: // UTF-8 multi-byte leading byte
		handleMultibyteRune(b, runes, buf)

	case b >= 0x20 && b < 0x7F: // Printable ASCII
		*runes = append(*runes, rune(b))
		_, _ = os.Stdout.Write(buf[:1]) // stdout write failure is unrecoverable
	}

	return "", false, nil
}

// eraseLineDisplay clears the entire line of runes from the terminal display.
func eraseLineDisplay(runes *[]rune) {
	if len(*runes) > 0 {
		totalWidth := 0
		for _, r := range *runes {
			totalWidth += runewidth.RuneWidth(r)
		}
		*runes = (*runes)[:0]
		fmt.Print(strings.Repeat("\b", totalWidth) + strings.Repeat(" ", totalWidth) + strings.Repeat("\b", totalWidth))
	}
}

// handleBackspace removes the last rune from the buffer and erases it from the display.
func handleBackspace(runes *[]rune) {
	if len(*runes) > 0 {
		removed := (*runes)[len(*runes)-1]
		*runes = (*runes)[:len(*runes)-1]
		w := runewidth.RuneWidth(removed)
		fmt.Print(strings.Repeat("\b", w) + strings.Repeat(" ", w) + strings.Repeat("\b", w))
	}
}

// handleMultibyteRune reads the remaining bytes of a multi-byte UTF-8 sequence
// and appends the decoded rune to the buffer.
func handleMultibyteRune(leadByte byte, runes *[]rune, buf []byte) {
	seqLen := utf8LeadByteLen(leadByte)
	buf[0] = leadByte
	for i := 1; i < seqLen; i++ {
		if _, err := os.Stdin.Read(buf[i : i+1]); err != nil {
			break
		}
	}
	if r, _ := utf8.DecodeRune(buf[:seqLen]); r != utf8.RuneError {
		*runes = append(*runes, r)
		_, _ = os.Stdout.Write(buf[:seqLen]) // stdout write failure is unrecoverable
	}
}

// isStandaloneEsc is called after reading an ESC byte (0x1B). It waits briefly
// to see if more bytes follow (indicating an escape sequence like an arrow key).
// Returns true for a standalone ESC press, false if an escape sequence was consumed.
func isStandaloneEsc() bool {
	ch := make(chan byte, 1)
	go func() {
		var b [1]byte
		if _, err := os.Stdin.Read(b[:]); err == nil {
			ch <- b[0]
		}
	}()

	select {
	case next := <-ch:
		if next == '[' {
			// CSI sequence (arrow keys, etc.) — consume until the final byte (0x40–0x7E)
			for {
				var b [1]byte
				if _, err := os.Stdin.Read(b[:]); err != nil {
					break
				}
				if b[0] >= 0x40 && b[0] <= 0x7E {
					break
				}
			}
		}
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}

// utf8LeadByteLen returns the expected total length of a UTF-8 sequence
// given its leading byte.
func utf8LeadByteLen(b byte) int {
	switch {
	case b < 0xE0:
		return 2
	case b < 0xF0:
		return 3
	default:
		return 4
	}
}
