package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strings"
)

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `helpme %s — AI shell-command help

Usage (with the bash/zsh shell hook installed):
  helpme <command>          Run a command; ask AI for a fix if it fails
  helpme "a question"       Ask AI without executing the question
  helpme --continue | -c [follow-up]
                            Continue this terminal's session (blank = elaborate)
  helpme --help | -h        Show usage and local connection diagnostics
  helpme --setup | -s       Configure a provider, model, and API key
  helpme --update | -u      Update to the latest release (shell hook only)
  helpme --version | -v     Print the version

Examples:
  helpme find -f notes.txt
  helpme "find files larger than 100 MB in this folder"
  helpme -c "explain the size flag"

A single argument containing spaces is a question. Other commands run as typed.
Suggestions appear on your next prompt for you to edit or run.
New requests replace the session. Help, setup, update, and version preserve it.

Direct binary usage (does not execute or prefill commands):
  helpme-bin --ask "a question"
  printf 'error text\n' | helpme-bin <failed command>
  helpme-bin --print-hook bash|zsh

Configuration:
  HELPME_PROVIDER    anthropic (default), openai, openrouter, or custom
  HELPME_MODEL       Model override (required for custom)
  HELPME_BASE_URL    OpenAI-compatible base URL (required for custom)
  HELPME_API_KEY     API key override (optional for custom/local servers)
  HELPME_REASONING   low, medium, high, minimal, or off
  HELPME_TIMEOUT     Request timeout in seconds (default: 30)
  HELPME_DEBUG=1     Log requests/responses to stderr (includes prompts)
  HELPME_LOG=<path>  Append requests/responses to a file (includes prompts)

Settings: environment > config file > built-in default.
Keys: HELPME_API_KEY > config api_key > selected provider's standard key variable.

Local connection diagnostics (no API request; credentials are never printed):
`, version)

	cfg, status := readConfigStatus()
	s := resolveProvider(cfg)
	p, err := providerFromSettings(s)
	fmt.Fprintf(w, "  Platform: %s/%s; shell: %q\n", runtime.GOOS, runtime.GOARCH, shellName())
	if executable, err := os.Executable(); err == nil {
		fmt.Fprintf(w, "  Binary: %q\n", executable)
	}
	pathSource := "HOME/.config (XDG_CONFIG_HOME is unset or blank)"
	if strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) != "" {
		pathSource = "XDG_CONFIG_HOME"
	}
	fmt.Fprintf(w, "  Config checked: %q (%s; via %s)\n", configPath(), status, pathSource)
	fmt.Fprintf(w, "  Selected provider: %q (%s)\n", s.name.value, s.name.source)
	fmt.Fprintf(w, "  Model: %q (%s)\n", s.model.value, s.model.source)
	fmt.Fprintf(w, "  Endpoint origin: %s (%s; user info, path, query, fragment omitted)\n", endpointOrigin(s.baseURL.value), s.baseURL.source)
	fmt.Fprintf(w, "  API key: %s\n", s.apiKey.source)
	if p.timeout > 0 {
		fmt.Fprintf(w, "  Reasoning: %q (%s; empty means omitted)\n", p.reasoning, s.reasoning.source)
		fmt.Fprintf(w, "  Timeout: %s\n", p.timeout)
	}
	if err != nil {
		fmt.Fprintf(w, "  Status: needs configuration — %s\n", err)
	} else {
		fmt.Fprintln(w, "  Status: configured locally; connectivity and authentication have not been verified")
		if s.name.value == "custom" && s.apiKey.value == "" {
			fmt.Fprintln(w, "  Custom endpoint will be called without an API key")
		}
	}
	fmt.Fprintln(w, "  Credential environment variables (presence only; only the selected source is used):")
	for _, name := range []string{"HELPME_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"} {
		state := "unset or blank"
		if strings.TrimSpace(os.Getenv(name)) != "" {
			state = "set"
		}
		fmt.Fprintf(w, "    %s: %s\n", name, state)
	}
	fmt.Fprintln(w, "  Environment variables are inherited; helpme cannot identify which shell file set them.")
	fmt.Fprintln(w, "  No .env files, Claude Code credentials, or Codex sign-in folders are searched.")
	fmt.Fprintln(w, "  Run helpme --setup to configure or change the connection.")
}

// Show only the origin: custom endpoints may embed secrets in user info,
// query parameters, fragments, or even path segments.
func endpointOrigin(raw string) string {
	if raw == "" {
		return "(not set)"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "(invalid URL; hidden)"
	}
	return u.Scheme + "://" + u.Host
}
