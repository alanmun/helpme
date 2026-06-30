// helpme — AI shell-command fixer (binary half).
//
// Reads a failed command (from args) plus its captured error text (from stdin),
// asks a configurable LLM provider for a correction, and prints exactly two
// lines to stdout:
//
//	line 1: the corrected command, ready to run
//	line 2: a short, plain-language explanation of the fix
//
// It also has an "ask" mode (--ask "<question>"): given a natural-language
// request, it prints the optional suggested command on line 1 (empty if none)
// and a formatted explanation on the lines after — a » prefix for a one-liner,
// or a box when it's multi-line. The shell wrapper routes a single quoted
// argument here instead of running it.
//
// The shell wrapper (hooks/helpme.{zsh,bash}) owns running the command and
// prefilling the corrected one onto the next prompt. This binary only performs
// the LLM round-trip, so it stays a small, testable unit with no shell smarts.
//
// On any failure (no key, bad provider, network/API error) it writes a message
// to stderr and exits non-zero; the wrapper falls back gracefully.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

// exitNoKey signals "not configured yet — no API key". The shell wrapper treats
// this exit code specially: the binary's own stderr message is actionable on its
// own, so the wrapper suppresses its generic failure line for it.
const exitNoKey = 3

func main() {
	args := os.Args[1:]

	// Sub-commands (only when they're the first argument, so they can't collide
	// with a real command the wrapper forwards).
	if len(args) > 0 {
		switch args[0] {
		case "setup", "--setup", "-s":
			runSetup()
			return
		case "--ask":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, `usage: helpme-bin --ask "<question>"`)
				os.Exit(2)
			}
			runAsk(strings.Join(args[1:], " "))
			return
		case "--print-hook":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: helpme-bin --print-hook zsh|bash")
				os.Exit(2)
			}
			printHook(args[1])
			return
		case "--version", "-v":
			fmt.Println("helpme", version)
			return
		}
	}

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

	p := loadProviderOrExit()

	f, err := askFix(p, command, errText)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		os.Exit(1)
	}

	fmt.Println(f.Cmd) // line 1
	fmt.Println(f.Why) // line 2
}

// runAsk handles "ask" mode: answer a natural-language question and, when it maps
// to one, suggest a command. Output mirrors fix mode so the wrapper can reuse its
// parsing — line 1 is the command (empty when there isn't one), the rest is the
// formatted explanation (a » line, or a box when multi-line).
func runAsk(query string) {
	p := loadProviderOrExit()

	a, err := askQuestion(p, query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		os.Exit(1)
	}

	fmt.Println(a.Command)                        // line 1: suggested command, or empty
	fmt.Println(renderExplanation(a.Explanation)) // line 2+: formatted explanation
}

// loadProviderOrExit resolves the provider or exits with an actionable message.
// errNoAPIKey gets the distinct exitNoKey code so the wrapper shows the binary's
// own setup hint alone, without piling its generic failure line on top.
func loadProviderOrExit() provider {
	p, err := loadProvider()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		if errors.Is(err, errNoAPIKey) {
			os.Exit(exitNoKey)
		}
		os.Exit(1)
	}
	return p
}
