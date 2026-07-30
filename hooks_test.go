package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the bash wrapper with a fake binary so the session lifecycle is
// tested without a provider or network call. zsh follows the same deliberately
// parallel implementation; not every development/CI machine has zsh installed.
func TestBashContinuationSession(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	tmp := t.TempDir()
	hookPath := filepath.Join(tmp, "helpme.bash")
	hook, err := hookFS.ReadFile("hooks/helpme.bash")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, hook, 0o600); err != nil {
		t.Fatal(err)
	}

	stubPath := filepath.Join(tmp, "helpme-bin")
	stub := `#!/usr/bin/env bash
case "$1" in
  --ask)
    printf "find . -name 'Xle*.mp3'\n"
    printf '» initial answer for %s\n' "$2"
    ;;
  --continue)
    saw_first=no
    saw_new=no
    case "$HELPME_SESSION_CONTEXT" in *"first session"*) saw_first=yes ;; esac
    case "$HELPME_SESSION_CONTEXT" in *"new session"*) saw_new=yes ;; esac
    printf 'continue|%s|%s|%s\n' "$2" "$saw_first" "$saw_new" >> "$HELPME_TEST_LOG"
    printf "find . -name 'Xle*narc*.mp3'\n"
    printf '» a more complete answer\nwith another detail\n'
    ;;
  *)
    exit 64
    ;;
esac
`
	if err := os.WriteFile(stubPath, []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(tmp, "events.log")
	script := `
set -e
source "$1"
__helpme_prefill() {
  printf 'prefill|%s\n' "$1" >> "$HELPME_TEST_LOG"
}
helpme "first session" >/dev/null
helpme -c "add narc" >/dev/null
helpme "new session" >/dev/null
helpme -c >/dev/null
`
	cmd := exec.Command(bash, "-c", script, "continuation-test", hookPath)
	cmd.Env = append(os.Environ(),
		"PATH="+tmp+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HELPME_TEST_LOG="+logPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash continuation scenario failed: %v\n%s", err, out)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(log)
	if !strings.Contains(got, "continue|add narc|yes|no\n") {
		t.Fatalf("first follow-up did not receive the initial session:\n%s", got)
	}
	if !strings.Contains(got, "continue||no|yes\n") {
		t.Fatalf("bare continuation or new-request reset was not preserved:\n%s", got)
	}
	if n := strings.Count(got, "prefill|find . -name 'Xle*narc*.mp3'\n"); n != 2 {
		t.Fatalf("revised continuation command was prefilled %d times, want 2:\n%s", n, got)
	}
}

func TestBashContinuationWithoutSession(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	tmp := t.TempDir()
	hookPath := filepath.Join(tmp, "helpme.bash")
	hook, err := hookFS.ReadFile("hooks/helpme.bash")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, hook, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bash, "-c", `source "$1"; helpme -c`, "continuation-test", hookPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("continuation without a session unexpectedly succeeded: %s", out)
	}
	if !strings.Contains(string(out), "nothing to continue") {
		t.Fatalf("missing actionable no-session error: %s", out)
	}
}
