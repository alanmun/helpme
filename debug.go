// debug.go — opt-in diagnostics so failures are debuggable after the fact.
//
// By default helpme keeps NO logs (your commands and prompts never touch disk).
// Two env vars turn logging on when you need it:
//
//	HELPME_DEBUG=1        write the full request/response exchange to stderr
//	HELPME_LOG=<path>     append the same, timestamped, to a file
//
// The Authorization header (your API key) is never logged — only the request
// body and the raw response — so a log is safe to paste when reporting a bug,
// though it does contain the command/question you asked about.
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	debugOnce   sync.Once
	debugStderr bool
	debugFile   *os.File
)

func debugSetup() {
	debugOnce.Do(func() {
		if truthy(os.Getenv("HELPME_DEBUG")) {
			debugStderr = true
		}
		if p := strings.TrimSpace(os.Getenv("HELPME_LOG")); p != "" {
			f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				fmt.Fprintf(os.Stderr, "helpme: cannot open HELPME_LOG %q: %v\n", p, err)
			} else {
				debugFile = f
			}
		}
	})
}

// debugf writes one diagnostic line to whichever sinks are enabled; a no-op when
// neither HELPME_DEBUG nor HELPME_LOG is set, so it's free on the hot path.
func debugf(format string, args ...any) {
	debugSetup()
	if !debugStderr && debugFile == nil {
		return
	}
	line := "[helpme " + time.Now().Format("2006-01-02 15:04:05") + "] " + fmt.Sprintf(format, args...) + "\n"
	if debugStderr {
		fmt.Fprint(os.Stderr, line)
	}
	if debugFile != nil {
		fmt.Fprint(debugFile, line)
	}
}

// since is a small helper for human-readable elapsed durations in messages.
func since(t time.Time) time.Duration { return time.Since(t).Round(time.Millisecond) }

// truthy treats unset/0/false/no/off as off and anything else as on.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
