// environment.go — tells the model what shell/OS it's actually fixing commands
// for, instead of letting it guess from surface cues (a Windows-style drive
// path, an unfamiliar error message) and default to the wrong tool family —
// e.g. suggesting cmd.exe's findstr for a user running bash under MSYS2.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// envDescription summarizes the user's shell and platform as one short clause,
// or "" if nothing could be determined (never blocks a request on this).
func envDescription() string {
	shell := shellName()
	platform := platformDescription()
	switch {
	case shell != "" && platform != "":
		return shell + " on " + platform
	case shell != "":
		return shell
	default:
		return platform
	}
}

// shellName prefers HELPME_SHELL, set explicitly by the bash/zsh wrapper that
// invoked this binary — the one fact known for certain, since each wrapper
// file is shell-specific. Falls back to $SHELL for direct/manual invocations.
func shellName() string {
	if s := strings.TrimSpace(os.Getenv("HELPME_SHELL")); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return filepath.Base(s)
	}
	return ""
}

// platformDescription reports the real command environment, not just GOOS. A
// Windows build is only ever installed under MSYS2, Git Bash, or Cygwin (see
// README) — a POSIX shell with GNU coreutils, NOT native cmd.exe/PowerShell —
// so it's called out explicitly rather than left to the model to assume.
func platformDescription() string {
	return platformDescriptionFor(runtime.GOOS)
}

// platformDescriptionFor takes GOOS as a parameter (rather than reading
// runtime.GOOS directly) so every branch is exercisable from tests, even
// though only one ever runs in a given build.
func platformDescriptionFor(goos string) string {
	switch goos {
	case "windows":
		if msystem := strings.TrimSpace(os.Getenv("MSYSTEM")); msystem != "" {
			return fmt.Sprintf("Windows via MSYS2/Git Bash (%s) — POSIX shell with GNU coreutils, NOT cmd.exe or PowerShell", msystem)
		}
		if strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "cygwin") {
			return "Windows via Cygwin — POSIX shell with GNU coreutils, NOT cmd.exe or PowerShell"
		}
		return "Windows via a POSIX shell (MSYS2/Git Bash/Cygwin) — GNU coreutils, NOT cmd.exe or PowerShell"
	case "linux":
		if distro := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")); distro != "" {
			return "Linux on WSL (" + distro + ")"
		}
		return "Linux"
	case "darwin":
		return "macOS"
	default:
		return goos
	}
}

// systemPromptWithEnv appends the detected environment to a base system
// prompt so fixes/suggestions use tools that actually exist there.
func systemPromptWithEnv(base string) string {
	env := envDescription()
	if env == "" {
		return base
	}
	return base + "\nThe user's actual environment: " + env + ". Only suggest commands and syntax that work there — never assume a different shell or OS from surface cues like a drive letter or path style."
}
