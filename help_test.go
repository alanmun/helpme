package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedHelpConfig(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HELPME_PROVIDER", "HELPME_API_KEY", "HELPME_MODEL", "HELPME_BASE_URL",
		"HELPME_REASONING", "HELPME_TIMEOUT", "HELPME_DEBUG", "HELPME_LOG",
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestHelpCredentialSources(t *testing.T) {
	for _, tc := range []struct {
		name, provider, standard, saved, override, source string
	}{
		{"missing", "anthropic", "", "", "", "none"},
		{"anthropic environment", "anthropic", "standard-secret", "", "", "environment: ANTHROPIC_API_KEY"},
		{"openai environment", "openai", "standard-secret", "", "", "environment: OPENAI_API_KEY"},
		{"openrouter environment", "openrouter", "standard-secret", "", "", "environment: OPENROUTER_API_KEY"},
		{"config wins over standard", "openai", "standard-secret", "saved-secret", "", "config file"},
		{"override wins over config", "openai", "standard-secret", "saved-secret", "override-secret", "environment: HELPME_API_KEY"},
		{"blank override falls back", "openai", "standard-secret", "saved-secret", " \t", "config file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedHelpConfig(t)
			t.Setenv("HELPME_PROVIDER", tc.provider)
			t.Setenv(providerKeyEnv[tc.provider], tc.standard)
			t.Setenv("HELPME_API_KEY", tc.override)
			if err := writeConfig(config{APIKey: tc.saved}); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			printHelp(&out)
			text := out.String()
			if !strings.Contains(text, "API key: "+tc.source+"\n") {
				t.Fatalf("missing credential source %q:\n%s", tc.source, text)
			}
			for _, secret := range []string{"standard-secret", "saved-secret", "override-secret"} {
				if strings.Contains(text, secret) {
					t.Fatal("help exposed a credential")
				}
			}
			p, err := loadProvider()
			if tc.source == "none" {
				if err == nil || !strings.Contains(text, "needs configuration") {
					t.Fatal("missing key must be diagnosed")
				}
			} else {
				want := firstNonEmpty(tc.override, tc.saved, tc.standard)
				if err != nil || p.apiKey != want || !strings.Contains(text, "configured locally") {
					t.Fatal("request provider and diagnostics disagree")
				}
			}
		})
	}
}

func TestHelpConfigAndOverrides(t *testing.T) {
	isolatedHelpConfig(t)
	if err := writeConfig(config{Provider: "openai", APIKey: "saved-secret", Model: "saved-model"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HELPME_PROVIDER", "custom")
	t.Setenv("HELPME_MODEL", "local-model")
	t.Setenv("HELPME_BASE_URL", "https://user:password-secret@example.com:8443/path-secret?key=query-secret#fragment-secret")
	t.Setenv("HELPME_REASONING", "off")
	t.Setenv("HELPME_TIMEOUT", "17")
	var out bytes.Buffer
	printHelp(&out)
	text := out.String()
	for _, want := range []string{
		configPath(), "loaded; via XDG_CONFIG_HOME", `Selected provider: "custom" (environment: HELPME_PROVIDER)`,
		`Model: "local-model" (environment: HELPME_MODEL)`, "Endpoint origin: https://example.com:8443",
		`Reasoning: "" (environment: HELPME_REASONING`, "Timeout: 17s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	for _, secret := range []string{"saved-secret", "password-secret", "path-secret", "query-secret", "fragment-secret"} {
		if strings.Contains(text, secret) {
			t.Fatal("help exposed a secret")
		}
	}
}

func TestHelpConfigProblems(t *testing.T) {
	for _, state := range []string{"not found", "invalid JSON", "unreadable"} {
		t.Run(state, func(t *testing.T) {
			isolatedHelpConfig(t)
			if state != "not found" {
				if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
					t.Fatal(err)
				}
				if state == "invalid JSON" {
					if err := os.WriteFile(configPath(), []byte(`{"api_key":"malformed-secret", broken}`), 0o600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(configPath(), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			printHelp(&out)
			if !strings.Contains(out.String(), state) || strings.Contains(out.String(), "malformed-secret") {
				t.Fatal("config problem was not safely diagnosed")
			}
		})
	}
}

func TestHelpHomeFallbackAndCustomWithoutKey(t *testing.T) {
	isolatedHelpConfig(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HELPME_PROVIDER", "custom")
	t.Setenv("HELPME_MODEL", "local-model")
	t.Setenv("HELPME_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("OPENAI_API_KEY", "unselected-secret")
	var out bytes.Buffer
	printHelp(&out)
	for _, want := range []string{filepath.Join(home, ".config", "helpme", "config.json"), "configured locally", "without an API key", "API key: none"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(out.String(), "unselected-secret") {
		t.Fatal("help exposed an inactive key")
	}
}

func TestEndpointOrigin(t *testing.T) {
	for _, raw := range []string{"https://user:secret@host/%invalid", "secret", "file:///secret", "https:///secret"} {
		if got := endpointOrigin(raw); got != "(invalid URL; hidden)" {
			t.Fatalf("unsafe invalid URL output: %q", got)
		}
	}
}

func TestHelpCLI(t *testing.T) {
	isolatedHelpConfig(t)
	t.Setenv("HELPME_PROVIDER", "unknown")
	dir := t.TempDir()
	binary := filepath.Join(dir, "helpme-bin")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, flag := range []string{"--help", "-h"} {
		cmd := exec.Command(binary, flag)
		if out, err := cmd.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("unknown provider")) || !bytes.Contains(out, []byte("Usage")) {
			t.Fatalf("help must succeed with a broken configuration: %v\n%s", err, out)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip(shell + " is not installed")
			}
			for _, flag := range []string{"--help", "-h"} {
				cmd := exec.Command(shell, "-c", `source "hooks/helpme.$1"; __helpme_session_context="existing-session"; helpme "$2"; rc=$?; test "$rc" -eq 0 && test "$__helpme_session_context" = "existing-session"`, "test", shell, flag)
				out, err := cmd.CombinedOutput()
				if err != nil || !bytes.Contains(out, []byte("Usage")) || !bytes.Contains(out, []byte(`shell: "`+shell+`"`)) {
					t.Fatalf("%s wrapper help: %v\n%s", shell, err, out)
				}
				if bytes.Contains(out, []byte("already works")) || bytes.Contains(out, []byte("could not get a fix")) {
					t.Fatal("help entered command execution mode")
				}
			}
		})
	}
}
