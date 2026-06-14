// config.go — persistent config so users don't have to keep env vars in their
// shell rc. `helpme setup` writes ~/.config/helpme/config.json (mode 0600,
// since it holds an API key); loadProvider reads it as the middle layer of the
// env > config > default precedence chain.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

type config struct {
	Provider string `json:"provider,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

// configPath honors XDG_CONFIG_HOME, falling back to ~/.config.
func configPath() string {
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "helpme", "config.json")
}

// readConfig returns an empty config if the file is missing or unreadable —
// helpme falls back to env vars and defaults in that case.
func readConfig() config {
	var c config
	b, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

func writeConfig(c config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600) // 0600: holds an API key
}

// runSetup is the interactive `helpme setup` wizard.
func runSetup() {
	in := bufio.NewReader(os.Stdin)
	cur := readConfig()

	fmt.Println("helpme setup")
	fmt.Println("Config will be saved to:", configPath())
	fmt.Println()

	provider := strings.ToLower(prompt(in, "Provider (anthropic/openai/openrouter/custom)", firstNonEmpty(cur.Provider, "anthropic")))
	if _, known := providerDefaults[provider]; !known && provider != "custom" {
		fmt.Fprintf(os.Stderr, "helpme: unknown provider %q\n", provider)
		os.Exit(2)
	}

	baseURL := cur.BaseURL
	if provider == "custom" {
		baseURL = prompt(in, "Base URL (OpenAI-compatible, e.g. http://localhost:11434/v1)", cur.BaseURL)
		if baseURL == "" {
			fmt.Fprintln(os.Stderr, "helpme: custom provider needs a base URL")
			os.Exit(2)
		}
	} else {
		baseURL = "" // use the built-in default for known providers
	}

	model := prompt(in, "Model", firstNonEmpty(cur.Model, providerDefaults[provider].model))

	key := promptSecret(in, "API key (blank to keep existing)")
	if key == "" {
		key = cur.APIKey
	}

	c := config{Provider: provider, APIKey: key, Model: model, BaseURL: baseURL}
	if err := writeConfig(c); err != nil {
		fmt.Fprintln(os.Stderr, "helpme: could not save config:", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Saved  provider=%s  model=%s  key=%s\n", c.Provider, c.Model, maskKey(c.APIKey))
	fmt.Println("Try:  helpme find -f myfile.txt")
}

func prompt(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// promptSecret reads without echoing when stdin is a terminal; otherwise it
// falls back to a plain line read (e.g. for piped/non-interactive input).
func promptSecret(in *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func maskKey(k string) string {
	if k == "" {
		return "(none)"
	}
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "…" + k[len(k)-4:]
}
