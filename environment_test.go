package main

import (
	"strings"
	"testing"
)

func TestPlatformDescriptionFor(t *testing.T) {
	t.Setenv("MSYSTEM", "")
	t.Setenv("OSTYPE", "")
	t.Setenv("WSL_DISTRO_NAME", "")

	// Windows without MSYSTEM/OSTYPE still calls out that it's POSIX, not
	// cmd/PowerShell, since that's the only environment helpme installs into.
	if got := platformDescriptionFor("windows"); !strings.Contains(got, "NOT cmd.exe or PowerShell") {
		t.Fatalf("windows default = %q, want it to rule out cmd/PowerShell", got)
	}

	t.Setenv("MSYSTEM", "UCRT64")
	if got := platformDescriptionFor("windows"); !strings.Contains(got, "MSYS2") || !strings.Contains(got, "UCRT64") {
		t.Fatalf("windows+MSYSTEM = %q, want it to name MSYS2 and the UCRT64 variant", got)
	}
	t.Setenv("MSYSTEM", "")

	t.Setenv("OSTYPE", "cygwin")
	if got := platformDescriptionFor("windows"); !strings.Contains(got, "Cygwin") {
		t.Fatalf("windows+cygwin OSTYPE = %q, want it to name Cygwin", got)
	}
	t.Setenv("OSTYPE", "")

	if got := platformDescriptionFor("darwin"); got != "macOS" {
		t.Fatalf("darwin = %q, want macOS", got)
	}

	if got := platformDescriptionFor("linux"); got != "Linux" {
		t.Fatalf("linux = %q, want Linux", got)
	}

	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if got := platformDescriptionFor("linux"); !strings.Contains(got, "WSL") || !strings.Contains(got, "Ubuntu") {
		t.Fatalf("linux+WSL = %q, want it to name WSL and the distro", got)
	}

	if got := platformDescriptionFor("plan9"); got != "plan9" {
		t.Fatalf("unknown goos = %q, want it echoed back verbatim", got)
	}
}

func TestShellName(t *testing.T) {
	t.Setenv("HELPME_SHELL", "zsh")
	t.Setenv("SHELL", "/bin/bash")
	if got := shellName(); got != "zsh" {
		t.Fatalf("shellName = %q, want HELPME_SHELL to win: zsh", got)
	}

	t.Setenv("HELPME_SHELL", "")
	if got := shellName(); got != "bash" {
		t.Fatalf("shellName fallback = %q, want basename of $SHELL: bash", got)
	}

	t.Setenv("SHELL", "")
	if got := shellName(); got != "" {
		t.Fatalf("shellName with nothing set = %q, want empty", got)
	}
}

func TestSystemPromptWithEnv(t *testing.T) {
	t.Setenv("HELPME_SHELL", "bash")
	t.Setenv("SHELL", "")

	got := systemPromptWithEnv("base prompt")
	if !strings.HasPrefix(got, "base prompt\n") {
		t.Fatalf("systemPromptWithEnv should keep the base prompt intact: %q", got)
	}
	if !strings.Contains(got, "bash on") {
		t.Fatalf("systemPromptWithEnv = %q, want it to name the shell", got)
	}
}
