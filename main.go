// helpme — AI shell-command fixer (binary half).
//
// Reads a failed command (from args) plus its captured error text (from stdin),
// asks a configurable LLM provider for a correction, and prints exactly two
// lines to stdout:
//
//	line 1: the corrected command, ready to run
//	line 2: a short, plain-language explanation of the fix
//
// The shell wrapper (hooks/helpme.{zsh,bash}) owns running the command and
// prefilling the corrected one onto the next prompt. This binary only performs
// the LLM round-trip, so it stays a small, testable unit with no shell smarts.
//
// On any failure (no key, bad provider, network/API error) it writes a message
// to stderr and exits non-zero; the wrapper falls back gracefully.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helpme: no command given")
		os.Exit(2)
	}
	command := strings.Join(args, " ")

	// Error text arrives on stdin (piped by the wrapper). If stdin is a TTY
	// (binary run by hand with no pipe), there simply is no error context.
	var errText string
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		b, _ := io.ReadAll(os.Stdin)
		errText = string(b)
	}

	p, err := loadProvider()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		os.Exit(1)
	}

	f, err := askFix(p, command, errText)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		os.Exit(1)
	}

	fmt.Println(f.Cmd) // line 1
	fmt.Println(f.Why) // line 2
}
